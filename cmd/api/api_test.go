package api_test

import (
	"bytes"
	"cmp"
	"io"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/api"
	"github.com/meshcloud/meshstack-cli/internal/apidocs"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const (
	apiDocsFile    = "../../client/openapi/testdata/spec.json"
	blockV1        = "application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json"
	blockV2Preview = "application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json"
)

func TestApi(t *testing.T) {
	apiDocs, err := os.ReadFile(apiDocsFile)
	require.NoError(t, err)
	meshStack := newFakeMeshStack(t, apiDocs)
	testlogin.LoggedInTo(t, meshStack.URL)
	postBlock := func(t *testing.T, body string, args ...string) (recordedRequest, error) {
		t.Helper()
		_, err := meshStack.run(t, api.New(), body, append([]string{"-X", "POST", "/api/meshobjects/meshbuildingblocks", "--request-json", "-"}, args...)...)
		if err != nil {
			assert.Empty(t, meshStack.requests)
			return recordedRequest{}, err
		}
		require.Len(t, meshStack.requests, 1)
		return meshStack.requests[0], nil
	}

	t.Run("sends the request and writes the answer as indented JSON", func(t *testing.T) {
		meshStack.answer(gohttp.StatusOK, `{"answer": 1}`)

		stdout, err := meshStack.run(t, api.New(), "",
			"-X", "delete", "/api/meshobjects/meshbuildingblocks/b1/purge?dry=true",
			"-H", "Accept: "+blockV2Preview)

		require.NoError(t, err)
		assert.Equal(t, "{\n  \"answer\": 1\n}\n", stdout)
		assert.Equal(t, []recordedRequest{{
			method:        gohttp.MethodDelete,
			pathAndQuery:  "/api/meshobjects/meshbuildingblocks/b1/purge?dry=true",
			accept:        blockV2Preview,
			authorization: "Bearer " + fakemeshstack.Token,
		}}, meshStack.requests)
	})

	t.Run("fails without a login before it downloads the API docs", func(t *testing.T) {
		t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
		t.Setenv(auth.ApiTokenSetting.EnvKey(), "")
		docsRequests := meshStack.docsRequests.Load()

		_, err := meshStack.run(t, api.New(), "", "/api/meshobjects/meshbuildingblocks")

		require.ErrorContains(t, err, "no credential for the endpoint")
		assert.Equal(t, docsRequests, meshStack.docsRequests.Load())
		assert.Empty(t, meshStack.requests)
	})

	t.Run("sends a meshObject in the latest version, preview included", func(t *testing.T) {
		request, err := postBlock(t, `{"spec": {}}`)

		require.NoError(t, err)
		assert.Equal(t, blockV2Preview, request.accept)
		assert.Equal(t, blockV2Preview, request.contentType)
		assert.JSONEq(t, `{"apiVersion": "v2-preview", "kind": "meshBuildingBlock", "spec": {}}`, request.body)
	})

	t.Run("sends a meshObject in the version --api-version, the Content-Type or the body names", func(t *testing.T) {
		for _, request := range []struct {
			body string
			args []string
		}{
			{`{"spec": {}}`, []string{"--api-version", "v1"}},
			{`{"spec": {}}`, []string{"-H", "Content-Type: " + blockV1}},
			{`{"apiVersion": "v1", "spec": {}}`, nil},
		} {
			sent, err := postBlock(t, request.body, request.args...)

			require.NoError(t, err)
			assert.Equal(t, blockV1, sent.contentType)
			assert.JSONEq(t, `{"apiVersion": "v1", "kind": "meshBuildingBlock", "spec": {}}`, sent.body)
		}
	})

	t.Run("refuses a body of no JSON, or one that names another version than --api-version", func(t *testing.T) {
		_, err := postBlock(t, `spec: {}`)
		require.EqualError(t, err, "--request-json - is not valid JSON")

		_, err = postBlock(t, `{"apiVersion": "v1"}`, "--api-version", "v2-preview")
		require.EqualError(t, err, "the Accept and Content-Type headers, --api-version and the body's apiVersion request different versions: v2-preview, v1")
	})

	t.Run("sends and asks for JSON on a path of no version, unless a header says otherwise", func(t *testing.T) {
		for _, header := range []string{"", "application/vnd.custom+json"} {
			args := []string{"-X", "POST", "/api/x", "--request-json", "-"}
			if header != "" {
				args = append(args, "-H", "Content-Type: "+header, "-H", "Accept: "+header)
			}

			_, err := meshStack.run(t, api.New(), `{"spec":{}}`, args...)

			require.NoError(t, err)
			require.Len(t, meshStack.requests, 1)
			assert.Equal(t, cmp.Or(header, "application/json"), meshStack.requests[0].accept)
			assert.Equal(t, cmp.Or(header, "application/json"), meshStack.requests[0].contentType)
			assert.JSONEq(t, `{"spec":{}}`, meshStack.requests[0].body)
		}
	})

	t.Run("fails with the error of meshStack, and with a hint to api-docs on a client error", func(t *testing.T) {
		for status, wantError := range map[int]string{
			gohttp.StatusConflict:   "POST " + meshStack.URL + "/api/x: meshStack answered HTTP 409: still in use\nRun meshstack api-docs /api/x -H 'Accept: application/json' -X POST to see what the API expects",
			gohttp.StatusBadGateway: "POST " + meshStack.URL + "/api/x: meshStack answered HTTP 502: still in use",
			gohttp.StatusForbidden:  "auth manual for " + meshStack.URL + ": POST " + meshStack.URL + "/api/x: meshStack answered HTTP 403: still in use",
		} {
			meshStack.answer(status, `{"message":"still in use"}`)

			stdout, err := meshStack.run(t, api.New(), "", "-X", "POST", "/api/x", "-H", "Accept: application/json")

			require.EqualError(t, err, wantError)
			assert.Empty(t, stdout)
		}
	})

	t.Run("points to api-docs on a 404 for a path the API docs do not list", func(t *testing.T) {
		meshStack.answer(gohttp.StatusNotFound, `{"errorCode":"NotFound","message":"gone"}`)

		_, err := meshStack.run(t, api.New(), "", "/api/x", "-H", "Accept: application/json")

		require.EqualError(t, err, "GET "+meshStack.URL+"/api/x: meshStack answered HTTP 404: gone (errorCode NotFound)\nRun meshstack api-docs /api/x -X GET -H 'Accept: application/json' to see what the API expects")
	})

	t.Run("adds the clientVersionHint of a failed answer to the error", func(t *testing.T) {
		meshStack.answer(gohttp.StatusNotFound, `{"errorCode":"NoHandlerFound","message":"no handler","errorId":"abc","clientVersionHint":"Upgrade the meshStack CLI to v0.9.0."}`)

		_, err := meshStack.run(t, api.New(), "", "/api/x", "-H", "Accept: application/json")

		require.EqualError(t, err, "GET "+meshStack.URL+"/api/x: meshStack answered HTTP 404: no handler (errorCode NoHandlerFound, errorId abc)\nUpgrade the meshStack CLI to v0.9.0.")
	})

	t.Run("asks for the media type the API docs list for a path of no version", func(t *testing.T) {
		meshStack.answer(gohttp.StatusOK, "plan")

		_, err := meshStack.run(t, api.New(), "", "/api/meshobjects/meshbuildingblockruns/abc/plan-artifact")

		require.NoError(t, err)
		require.Len(t, meshStack.requests, 1)
		assert.Equal(t, "application/octet-stream", meshStack.requests[0].accept)
	})

	t.Run("writes an answer of no JSON as it came, ending text with a newline", func(t *testing.T) {
		for answer, want := range map[string]string{"plain text": "plain text\n", "PK\x03\x04\xff": "PK\x03\x04\xff"} {
			meshStack.answer(gohttp.StatusOK, answer)

			stdout, err := meshStack.run(t, api.New(), "", "/api/x")

			require.NoError(t, err)
			assert.Equal(t, want, stdout)
		}
	})

	t.Run("sends no token to another host", func(t *testing.T) {
		for _, target := range []string{"https://example.com/api/x", "//example.com/api/x"} {
			_, err := meshStack.run(t, api.New(), "", target)

			require.ErrorContains(t, err, "example.com")
			assert.Empty(t, meshStack.requests)
		}
	})

	t.Run("api-docs lists every operation, or writes the whole document as JSON", func(t *testing.T) {
		stdout, err := meshStack.run(t, api.NewDocs(), "")

		require.NoError(t, err)
		assert.Contains(t, stdout, "| POST | `/api/meshobjects/meshbuildingblocks` | v1, v2-preview |")
		assert.Contains(t, stdout, "| GET, DELETE | `/api/meshobjects/meshtenants/{tenant}` | v3, v4 |")

		stdout, err = meshStack.run(t, api.NewDocs(), "", "-o", "json")

		require.NoError(t, err)
		assert.JSONEq(t, string(apiDocs), stdout)
	})

	t.Run("api-docs lists the operations of the method and version the flags name", func(t *testing.T) {
		stdout, err := meshStack.run(t, api.NewDocs(), "", "-X", "DELETE")

		require.NoError(t, err)
		assert.Equal(t, "| Methods | Path | API versions |\n|---|---|---|\n| DELETE | `/api/meshobjects/meshtenants/{tenant}` |  |\n", stdout)

		_, err = meshStack.run(t, api.NewDocs(), "", "-X", "PATCH")

		require.EqualError(t, err, "the API docs have no operation for PATCH")
	})

	t.Run("api-docs describes the request of a command line of api", func(t *testing.T) {
		for _, request := range []struct {
			body, wantVersion string
			args              []string
		}{
			{"", "v2-preview", []string{"/api/meshobjects/meshbuildingblocks?page=2"}},
			{"", "v1", []string{"-X", "POST", "/api/meshobjects/meshbuildingblocks", "--api-version", "v1"}},
			{"", "v1", []string{"-X", "POST", "/api/meshobjects/meshbuildingblocks", "-H", "Accept: " + blockV1}},
			{`{"apiVersion": "v1"}`, "v1", []string{"-X", "POST", "/api/meshobjects/meshbuildingblocks", "--request-json", "-"}},
		} {
			meshStack.answer(gohttp.StatusOK, `{}`)
			_, err := meshStack.run(t, api.New(), request.body, request.args...)
			require.NoError(t, err)

			stdout, err := meshStack.run(t, api.NewDocs(), request.body, request.args...)

			require.NoError(t, err)
			assert.Contains(t, stdout, "# meshBuildingBlock\n\n## create: `POST /api/meshobjects/meshbuildingblocks`\n\n"+
				"API version "+request.wantVersion+"\n\n**Create building block**\n\n"+
				"Create a new meshBuildingBlock with inputs and parent building block relationships\n\n### Request body", request.args)
			assert.Contains(t, stdout, "### Request body `application/vnd.meshcloud.api.meshbuildingblock."+request.wantVersion+".hal+json`", request.args)
		}
	})

	t.Run("api-docs describes every method of a path, unless --method names one", func(t *testing.T) {
		stdout, err := meshStack.run(t, api.NewDocs(), "", "/api/meshobjects/meshtenants/abc")

		require.NoError(t, err)
		assert.Equal(t, "# meshTenant\n\n"+
			"## show: `GET /api/meshobjects/meshtenants/{tenant}`\n\nAPI version v4\n\n**Get tenant**\n\n"+
			"### Parameters\n\n| Name | In | Type | Required | Description |\n|---|---|---|---|---|\n"+
			"| `tenant` | path | string | yes |  |\n\n"+
			"### Response 200 `application/vnd.meshcloud.api.meshtenant.v4.hal+json`\n\n"+
			"## delete: `DELETE /api/meshobjects/meshtenants/{tenant}`\n\nNo API version\n\n**Delete tenant**\n\n"+
			"### Parameters\n\n| Name | In | Type | Required | Description |\n|---|---|---|---|---|\n"+
			"| `tenant` | path | string | yes | DELETE takes the UUID or the full tenant identifier. |\n"+
			"| `purge` | query | boolean |  | Removes the meshTenant from meshStack at once. |\n\n"+
			"### Response 202\n\n"+
			"### Response 400 `application/json`\n\n| Field | Type | Required | Description |\n|---|---|---|---|\n"+
			"| `errorCode` | string | yes |  |\n| `message` | string | yes | A message that describes the error. |\n", stdout)

		stdout, err = meshStack.run(t, api.NewDocs(), "", "-X", "GET", "/api/meshobjects/meshtenants/abc", "--api-version", "v3")

		require.NoError(t, err)
		assert.Contains(t, stdout, "## show: `GET /api/meshobjects/meshtenants/{tenant}`\n\nAPI version v3\n")
		assert.Contains(t, stdout, "### Response 200 `application/vnd.meshcloud.api.meshtenant.v3.hal+json`\n")
		assert.NotContains(t, stdout, "DELETE")
	})

	t.Run("api-docs writes the part of the document a request selects as JSON", func(t *testing.T) {
		stdout, err := meshStack.run(t, api.NewDocs(), "", "/api/meshobjects/meshtenants/abc", "-X", "GET", "-o", "json")

		require.NoError(t, err)
		assert.JSONEq(t, `{"paths": {"/api/meshobjects/meshtenants/{tenant}": {"get": {
			"tags": ["Tenants"],
			"summary": "Get tenant",
			"operationId": "meshTenantGet",
			"parameters": [{"name": "tenant", "in": "path", "required": true, "schema": {"type": "string"}, "x-meshstack-path-key": "metadata.uuid", "x-meshstack-path-key-by-version": {"v3": "metadata.tenantIdentifier"}}],
			"responses": {"200": {"description": "The tenant", "content": {"application/vnd.meshcloud.api.meshtenant.v4.hal+json": {"x-meshstack-lifecycle": {"meshObject": "meshTenant.v4", "state": "GA"}}}}},
			"x-position": 110
		}}}}`, stdout)

		_, err = meshStack.run(t, api.NewDocs(), "", "/api/meshobjects/meshunknowns")

		require.EqualError(t, err, "the API docs have no operation for /api/meshobjects/meshunknowns")
	})

	t.Run("api-docs --describe writes the latest version of each operation of a kind as Markdown", func(t *testing.T) {
		stdout, err := meshStack.run(t, api.NewDocs(), "", "--describe", "bb")

		require.NoError(t, err)
		assert.Contains(t, stdout, "# meshBuildingBlock\n\n## create: `POST /api/meshobjects/meshbuildingblocks`\n\nAPI version v2-preview\n\n**Create building block**\n\n"+
			"Create a new meshBuildingBlock with inputs and parent building block relationships\n\n### Request body `"+blockV2Preview+"`")
		assert.Contains(t, stdout, "### Response 201 `"+blockV2Preview+"`")
		assert.Contains(t, stdout, "### Response 400 `application/json`")

		stdout, err = meshStack.run(t, api.NewDocs(), "", "--describe", "tenant.delete", "-o", "json")

		require.NoError(t, err)
		assert.JSONEq(t, `{
			"paths": {"/api/meshobjects/meshtenants/{tenant}": {"delete": {
				"tags": ["Tenants"],
				"summary": "Delete tenant",
				"operationId": "meshTenantDelete",
				"parameters": [
					{"name": "tenant", "in": "path", "description": "DELETE takes the UUID or the full tenant identifier.", "required": true, "schema": {"type": "string"}, "x-meshstack-path-key": "metadata.uuid"},
					{"name": "purge", "in": "query", "description": "Removes the meshTenant from meshStack at once.", "required": false, "schema": {"type": "boolean"}}
				],
				"responses": {
					"202": {"description": "Deletion of tenant started"},
					"400": {"description": "The request is invalid. The message names the invalid part.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/CustomErrorMessage"}}}}
				},
				"x-position": 112
			}}},
			"components": {"schemas": {"CustomErrorMessage": {
				"required": ["errorCode", "message"],
				"type": "object",
				"properties": {"errorCode": {"type": "string"}, "message": {"type": "string", "description": "A message that describes the error."}},
				"description": "The body of every error response."
			}}}
		}`, stdout)
	})

	t.Run("api-docs --describe takes a kind of the API docs, and neither a path nor a request", func(t *testing.T) {
		_, err := meshStack.run(t, api.NewDocs(), "", "--describe", "workspace")
		require.EqualError(t, err, `the API docs have no meshObject kind "workspace"; use one of buildingblock, buildingblockrun, tenant`)

		_, err = meshStack.run(t, api.NewDocs(), "", "--describe", "tenant.list")
		require.EqualError(t, err, `the API docs have no action "list" for meshTenant; use one of show, delete`)

		_, err = meshStack.run(t, api.NewDocs(), "", "--describe", "bb", "/api/meshobjects/meshbuildingblocks")
		require.EqualError(t, err, "--describe cannot be combined with a path; see --help")

		_, err = meshStack.run(t, api.NewDocs(), "", "--describe", "bb", "--api-version", "v1")
		require.ErrorContains(t, err, "[api-version describe] were all set")
	})

	t.Run("without a writable config directory, api sends the request as given and api-docs fails", func(t *testing.T) {
		dir := os.Getenv(config.DirectorySetting.EnvKey())
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "api-docs")), "the earlier steps stored the API docs") //nolint:gosec // G703: the config directory of this test
		// G302 counts a directory's x bit, without which nothing can enter it.
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // G302: see above
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: see above
		captured := logs.Capture(t)
		meshStack.answer(gohttp.StatusOK, `{}`)

		_, err := meshStack.run(t, api.New(), `{"spec": {}}`, "-X", "POST", "/api/meshobjects/meshbuildingblocks", "--request-json", "-")

		require.NoError(t, err)
		require.Len(t, meshStack.requests, 1)
		assert.Empty(t, meshStack.requests[0].accept, "a meshObject endpoint answers application/json with a 406 that names no version")
		assert.Equal(t, "application/json", meshStack.requests[0].contentType)
		assert.Contains(t, captured.String(), "Sending the request unchanged, because the API docs are not available")
		assert.Contains(t, captured.String(), "Run meshstack api-docs /api/meshobjects/meshbuildingblocks -X POST to see why.")

		_, err = meshStack.run(t, api.NewDocs(), "")

		require.ErrorIs(t, err, apidocs.ErrNoConfigDir)
		assert.ErrorContains(t, err, dir+" is not writable, set MESHSTACK_CONFIG_DIR to a directory that is")
	})
}

type recordedRequest struct {
	method, pathAndQuery, accept, contentType, authorization, body string
}

type fakeMeshStack struct {
	*fakemeshstack.Server

	requests     []recordedRequest
	docsRequests atomic.Int32
}

func newFakeMeshStack(t *testing.T, apiDocs []byte) *fakeMeshStack {
	t.Helper()
	meshStack := &fakeMeshStack{Server: fakemeshstack.Start(t, fakemeshstack.Options{})}
	serveApiDocs := fakemeshstack.ServeApiDocs(apiDocs)
	meshStack.Route("GET "+fakemeshstack.ApiDocsPath, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		meshStack.docsRequests.Add(1)
		serveApiDocs(w, r)
	})
	meshStack.answer(gohttp.StatusOK, `{}`)
	return meshStack
}

func (f *fakeMeshStack) answer(status int, body string) {
	f.Route("/", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *fakeMeshStack) run(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	f.TakeRequests()
	var stdout bytes.Buffer
	// as the root command does
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	err := cmd.ExecuteContext(t.Context())
	f.requests = nil
	for _, r := range f.TakeRequests() {
		if r.URL.Path == fakemeshstack.ApiDocsPath {
			continue
		}
		f.requests = append(f.requests, recordedRequest{
			method:        r.Method,
			pathAndQuery:  r.URL.RequestURI(),
			accept:        strings.Join(r.Header["Accept"], ","),
			contentType:   strings.Join(r.Header["Content-Type"], ","),
			authorization: strings.Join(r.Header["Authorization"], ","),
			body:          string(r.Body),
		})
	}
	return stdout.String(), err
}
