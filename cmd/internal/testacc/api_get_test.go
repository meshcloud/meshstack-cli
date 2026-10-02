package testacc

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/apidocs"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

// getExceptions adapt the GET of a path template where the API docs do not say enough to send it,
// or skip it.
var getExceptions = map[openapi.PathTemplate]getException{
	"/api/meshobjects/meshbuildingblockdefinitionversions": {
		query: map[string]objectField{"buildingBlockDefinitionUuid": {"meshBuildingBlockDefinition", "uuid"}},
	},
	"/api/meshobjects/meshbuildingblockruns": {
		query: map[string]objectField{"buildingBlockUuid": {"meshBuildingBlock", "uuid"}},
	},
	"/api/meshobjects/meshapikeys/self": {
		apiKeyOnly: "it answers 404 to a request that authenticates with no meshApiKey",
	},
	"/api/meshobjects/meshbuildingblockruns/{blockRunUuid}/plan-artifact": {
		skip: "the API docs say it is in development, and answers 404 in normal operation",
	},
	"/api/meshobjects/meshprojects/{fullProjectIdentifier}": {
		parameters: map[string]string{"fullProjectIdentifier": "{ownedByWorkspace}.{name}"},
	},
	"/api/meshobjects/meshworkspaceusergroups/{fullWorkspaceUserGroupIdentifier}": {
		parameters: map[string]string{"fullWorkspaceUserGroupIdentifier": "{ownedByWorkspace}.{name}"},
	},
}

type getException struct {
	// query fills a query parameter that the list requires, and the API docs do not mark as required.
	query map[string]objectField
	// parameters give the value of a path parameter that fill does not find on its own, with each
	// metadata field in braces.
	parameters map[string]string
	// skip sends no GET, for the reason it gives.
	skip string
	// apiKeyOnly sends the GET only for the API key login, for the reason it gives.
	apiKeyOnly string
}

// objectField is a field of the metadata of the first object of a kind.
type objectField struct {
	kind, field string
}

func everyGetOperationAnswers(c *cli, apiKey bool) func(*testing.T) {
	return func(t *testing.T) {
		spec := devApiDocs(t)
		objects := firstObjects{spec: spec, meshStack: c.client(t)}
		gets, err := spec.Select(openapi.Selector{Method: gohttp.MethodGet})
		require.NoError(t, err)
		require.NotEmpty(t, gets.Operations, "the API docs list no GET operation, so this test would pass without sending one")
		for _, operation := range gets.Operations {
			t.Run(strings.TrimSpace("GET "+string(operation.PathTemplate)+" "+operation.LatestApiVersion().String()), func(t *testing.T) {
				t.Parallel()
				exception := getExceptions[operation.PathTemplate]
				if exception.skip != "" {
					t.Skip(exception.skip)
				}
				if exception.apiKeyOnly != "" && !apiKey {
					t.Skip(exception.apiKeyOnly)
				}
				r, skip, err := objects.fill(t.Context(), operation, exception)
				require.NoError(t, err)
				if skip != "" {
					t.Skip(skip)
				}
				status, body, err := objects.get(t.Context(), r, operation)
				require.NoError(t, err)
				assert.Truef(t, status >= 200 && status < 300, "GET %s answered %d: %s", r, status, body)
			})
		}
	}
}

type organizationAdminLogin struct {
	devLogin

	workspace string
}

func organizationAdmin(t *testing.T, logins []devLogin) organizationAdminLogin {
	t.Helper()
	for _, login := range logins {
		for workspace, role := range login.Workspaces {
			if role == "Organization Admin" {
				return organizationAdminLogin{login, workspace}
			}
		}
	}
	t.Fatalf("%s carries no login with the role Organization Admin", envTestUsers)
	return organizationAdminLogin{}
}

func devApiDocs(t *testing.T) openapi.Spec {
	t.Helper()
	t.Setenv(envConfigDir, t.TempDir())
	spec, wait, err := apidocs.Load(t.Context(), apidocs.Options{
		Sources:   internal.SettingSources(),
		UserAgent: "meshstack-cli/testacc",
		Version:   internal.Version,
		Dev:       true,
	})
	wait()
	require.NoError(t, err)
	return spec
}

// client resolves the credential the login stored, as any command after it would.
func (c *cli) client(t *testing.T) client.Client {
	t.Helper()
	c.applyEnv()
	meshStack, err := auth.ResolveClient(t.Context(), auth.ResolveClientOptions{
		Version:    "testacc",
		GitHubRepo: "meshcloud/meshstack-cli",
	})
	require.NoError(t, err)
	return meshStack
}

type getRequest struct {
	path  string
	query url.Values
}

func (r getRequest) String() string {
	if len(r.query) == 0 {
		return r.path
	}
	return r.path + "?" + r.query.Encode()
}

// firstObjects lists each kind once, in its latest version, and keeps the first object of the list,
// for GETs that run in parallel. It returns errors rather than fail a test, because the test that
// lists a kind first may not be the one the object is for.
type firstObjects struct {
	spec      openapi.Spec
	meshStack client.Client

	mu     sync.Mutex
	listed map[string]func() listed
}

type listed struct {
	metadata map[string]string
	// none says why there is no metadata.
	none string
}

func (o *firstObjects) get(ctx context.Context, r getRequest, operation openapi.Operation) (int, string, error) {
	options := []http.RequestOption{http.WithUrlQuery(r.query)}
	if mediaType, ok := operation.LatestMediaType(); ok {
		options = append(options, http.WithAccept(mediaType.Name))
	}
	body, err := o.meshStack.Raw.DoRequest(ctx, gohttp.MethodGet, r.path, options...)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok {
		return httpErr.StatusCode, string(httpErr.ResponseBody), nil
	}
	return gohttp.StatusOK, string(body), err
}

// fill replaces each path parameter by a field of the first object's metadata. skip says why there
// is no request to send.
func (o *firstObjects) fill(ctx context.Context, operation openapi.Operation, exception getException) (r getRequest, skip string, err error) {
	segments := strings.Split(string(operation.PathTemplate), "/")
	for i, segment := range segments {
		parameter, isParameter := strings.CutPrefix(segment, "{")
		if !isParameter {
			continue
		}
		parameter = strings.TrimSuffix(parameter, "}")
		first := o.first(ctx, operation.Kind)
		if first.metadata == nil {
			return r, first.none, nil
		}
		value, err := parameterValue(parameter, first.metadata, exception)
		if err != nil {
			return r, "", fmt.Errorf("%s of %s: %w", segment, operation.Kind, err)
		}
		segments[i] = url.PathEscape(value)
	}
	r.path = strings.Join(segments, "/")
	for parameter, from := range exception.query {
		first := o.first(ctx, from.kind)
		if first.metadata[from.field] == "" {
			return r, cmp.Or(first.none, fmt.Sprintf("the first %s has no metadata.%s", from.kind, from.field)), nil
		}
		if r.query == nil {
			r.query = url.Values{}
		}
		r.query.Set(parameter, first.metadata[from.field])
	}
	return r, "", nil
}

var metadataFieldRe = regexp.MustCompile(`\{(\w+)\}`)

// parameterValue takes the field of the metadata whose name the parameter ends with, longest first,
// so that blockRunUuid takes metadata.uuid. A parameter that ends with identifier or id and with no
// field takes metadata.name, which identifies most kinds.
func parameterValue(parameter string, metadata map[string]string, exception getException) (string, error) {
	if template, ok := exception.parameters[parameter]; ok {
		var err error
		value := metadataFieldRe.ReplaceAllStringFunc(template, func(field string) string {
			value := metadata[strings.Trim(field, "{}")]
			if value == "" {
				err = fmt.Errorf("the metadata has no %s: %v", field, metadata)
			}
			return value
		})
		return value, err
	}
	lower := strings.ToLower(parameter)
	var field string
	for name := range metadata {
		if strings.HasSuffix(lower, strings.ToLower(name)) && len(name) > len(field) {
			field = name
		}
	}
	if field == "" && (strings.HasSuffix(lower, "identifier") || strings.HasSuffix(lower, "id")) {
		field = "name"
	}
	if metadata[field] == "" {
		return "", fmt.Errorf("no field of the metadata fills it: %v", metadata)
	}
	return metadata[field], nil
}

// first lists a kind outside the lock, because the list of one kind can take its query from the
// first object of another.
func (o *firstObjects) first(ctx context.Context, kind string) listed {
	o.mu.Lock()
	if o.listed == nil {
		o.listed = map[string]func() listed{}
	}
	list, ok := o.listed[kind]
	if !ok {
		list = sync.OnceValue(func() listed { return o.list(ctx, kind) })
		o.listed[kind] = list
	}
	o.mu.Unlock()
	return list()
}

func (o *firstObjects) list(ctx context.Context, kind string) listed {
	selected, err := o.spec.Select(openapi.Selector{Kind: kind, Action: "list"})
	if err != nil {
		return listed{none: err.Error()}
	}
	if len(selected.Operations) == 0 {
		return listed{none: "the API docs list no list of " + kind}
	}
	list := selected.Operations[0]
	r, skip, err := o.fill(ctx, list, getExceptions[list.PathTemplate])
	if err != nil || skip != "" {
		return listed{none: cmp.Or(skip, fmt.Sprint(err))}
	}
	status, body, err := o.get(ctx, r, list)
	if err != nil {
		return listed{none: fmt.Sprintf("the list of %s failed: %s", kind, err)}
	}
	if status != gohttp.StatusOK {
		return listed{none: fmt.Sprintf("the list of %s answered %d", kind, status)}
	}
	var page struct {
		Embedded map[string][]struct {
			Metadata map[string]jsontext.Value `json:"metadata"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		return listed{none: fmt.Sprintf("the list of %s does not decode: %s", kind, err)}
	}
	for _, objects := range page.Embedded {
		if len(objects) == 0 {
			continue
		}
		metadata := map[string]string{}
		for name, value := range objects[0].Metadata {
			var text string
			if json.Unmarshal(value, &text) == nil {
				metadata[name] = text
			}
		}
		return listed{metadata: metadata}
	}
	return listed{none: fmt.Sprintf("the list of %s answered no object", kind)}
}
