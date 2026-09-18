package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type HttpClient struct {
	http.AuthorizedClient

	EndpointUrl xurl.URL
}

// MeshObjectClient provides typed CRUD operations for meshStack API objects.
// It embeds [http.AuthorizedClient] and adds meshObject-specific functionality including automatic
// MIME type handling and pagination.
type MeshObjectClient[M any] struct {
	http.AuthorizedClient

	Kind       string
	ApiVersion string
	ApiUrl     *url.URL
}

// NewMeshObjectClient creates a new [MeshObjectClient] for a specific meshObject type with automatic URL path inference.
// The meshObject kind is inferred from type M.
// The API URL is constructed from explicitApiPathElems if provided,
// otherwise the pluralized and lowercased kind is used as a single element, a convention only the
// workspace and project user/group binding APIs break.
func NewMeshObjectClient[M any](ctx context.Context, httpClient HttpClient, apiVersion string, explicitApiPathElems ...string) MeshObjectClient[M] {
	kind := InferKind[M]()

	if len(explicitApiPathElems) == 0 {
		explicitApiPathElems = []string{strings.ToLower(pluralizeKind(kind))}
	}
	explicitApiPathElems = slices.Insert(explicitApiPathElems, 0, "/api/meshobjects")
	apiUrl := httpClient.EndpointUrl.JoinPath(explicitApiPathElems...)
	slog.DebugContext(ctx, fmt.Sprintf("initialized %s client", reflect.TypeFor[M]().Name()), "url", apiUrl.String(), "kind", kind, "version", apiVersion)
	return MeshObjectClient[M]{httpClient.AuthorizedClient, kind, apiVersion, apiUrl}
}

var versionSuffixRe = regexp.MustCompile(`V\d+$`)

// InferKind infers the meshObject kind from a struct type name using the same convention
// as the meshObject API: MeshWorkspace → "meshWorkspace", MeshBuildingBlockV2 → "meshBuildingBlock".
// Version suffixes (V\d+) are stripped.
// Tested when client.Kind is statically initialized.
func InferKind[M any]() string {
	typeName := reflect.TypeFor[M]().Name()

	runes := []rune(typeName)
	runes[0] = unicode.ToLower(runes[0])
	kind := string(runes)

	return versionSuffixRe.ReplaceAllString(kind, "")
}

var pluralExceptions = map[string]string{
	// Add exceptions here as needed, e.g. "meshPolicy": "meshPolicies"
}

func pluralizeKind(kind string) string {
	if plural, ok := pluralExceptions[kind]; ok {
		return plural
	}
	return kind + "s"
}

func (c MeshObjectClient[M]) MeshObjectMimeType() string {
	return fmt.Sprintf("application/vnd.meshcloud.api.%s.%s.hal+json", c.Kind, c.ApiVersion)
}

// Get retrieves a meshObject by ID. Returns nil if not found.
func (c MeshObjectClient[M]) Get(ctx context.Context, id string) (resp *M, err error) {
	resp, err = c.GetAtPath[*M](ctx, id)
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		//nolint:nilnil // MeshObject clients return nil on 404, which is how Terraform handles a resource that does not exist
		return nil, nil
	}
	return
}

func (c MeshObjectClient[M]) GetAtPath[R any](ctx context.Context, id string, extraPath ...string) (R, error) {
	return c.DoRequest[R](ctx, http.MethodGet, c.ApiUrl.JoinPath(id).JoinPath(extraPath...), http.WithAccept(c.MeshObjectMimeType()))
}

// Post creates a new meshObject with the given payload.
// Automatically injects apiVersion and kind into the JSON payload.
func (c MeshObjectClient[M]) Post[P any](ctx context.Context, payload P) (*M, error) {
	return c.PostAtPath[*M](ctx, payload)
}

func (c MeshObjectClient[M]) PostAtPath[R, P any](ctx context.Context, payload P, extraPath ...string) (R, error) {
	return c.DoRequest[R](ctx, http.MethodPost, c.ApiUrl.JoinPath(extraPath...),
		http.WithAccept(c.MeshObjectMimeType()), c.withMeshObjectPayload(payload))
}

// Put updates an existing meshObject by ID with the given payload.
// Automatically injects apiVersion and kind into the JSON payload.
func (c MeshObjectClient[M]) Put[P any](ctx context.Context, id string, payload P) (*M, error) {
	return c.DoRequest[*M](ctx, http.MethodPut, c.ApiUrl.JoinPath(id), c.withMeshObjectPayload(payload), http.Retryable())
}

// withMeshObjectPayload injects apiVersion and kind as top-level members. P carries the payload's
// own type because the `,embed` tag takes a struct, a string-keyed map or a jsontext.Value, never
// an any.
func (c MeshObjectClient[M]) withMeshObjectPayload[P any](payload P) http.RequestOption {
	return http.WithJsonPayload(struct {
		ApiVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Payload    P      `json:",embed"`
	}{c.ApiVersion, c.Kind, payload}, c.MeshObjectMimeType())
}

// Delete removes a meshObject by ID.
func (c MeshObjectClient[M]) Delete(ctx context.Context, id string) (err error) {
	return c.DeleteAtPath(ctx, id)
}

func (c MeshObjectClient[M]) DeleteAtPath(ctx context.Context, id string, extraPath ...string) (err error) {
	_, err = c.DoRequest[any](ctx, http.MethodDelete, c.ApiUrl.JoinPath(id).JoinPath(extraPath...), http.Retryable(), http.WithAccept(c.MeshObjectMimeType()))
	return
}

// List retrieves all meshObjects with automatic pagination handling.
// Accepts optional [http.RequestOption] parameters for filtering and querying.
func (c MeshObjectClient[M]) List(ctx context.Context, options ...http.RequestOption) ([]M, error) {
	var result []M
	embeddedKey := pluralizeKind(c.Kind)
	pageNumber := 0

	for {
		type paginatedResponse struct {
			Embedded map[string][]M `json:"_embedded"`
			Page     struct {
				TotalPages int `json:"totalPages"`
				Number     int `json:"number"`
			} `json:"page"`
		}
		response, err := c.DoRequest[paginatedResponse](ctx, http.MethodGet, c.ApiUrl, append(options,
			http.WithAccept(c.MeshObjectMimeType()),
			http.WithUrlQuery(map[string]any{"page": pageNumber}),
		)...)
		if err != nil {
			return result, fmt.Errorf("error getting page %d: %w", pageNumber, err)
		} else if items, ok := response.Embedded[embeddedKey]; !ok {
			return result, fmt.Errorf("embedded key %s not found in paginated response", embeddedKey)
		} else {
			result = append(result, items...)
		}
		if response.Page.Number >= response.Page.TotalPages-1 {
			return result, nil
		}
		pageNumber++
	}
}
