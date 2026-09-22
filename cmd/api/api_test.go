package api_test

import (
	"bytes"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/api"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// unexpiringJwt is an unsigned JWT that expires in 2100, so the CLI sends it rather than asking for a new one.
const unexpiringJwt = "eyJhbGciOiJub25lIn0.eyJleHAiOjQxMDI0NDQ4MDB9."

type recordedRequest struct {
	method, pathAndQuery, accept, contentType, authorization, body string
}

func TestApiSendsTheRequestAndWritesTheAnswerAsItCame(t *testing.T) {
	var requests []recordedRequest
	endpoint := fakeMeshStack(t, &requests, gohttp.StatusOK, `{"answer": 1}`)

	stdout, err := runApi(t, endpoint, "",
		"-X", "delete", "/api/meshobjects/meshbuildingblocks/b1/purge?dry=true",
		"-H", "Accept: application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json")

	require.NoError(t, err)
	assert.Equal(t, `{"answer": 1}`, stdout, "the answer is not reformatted")
	assert.Equal(t, []recordedRequest{{
		method:        gohttp.MethodDelete,
		pathAndQuery:  "/api/meshobjects/meshbuildingblocks/b1/purge?dry=true",
		accept:        "application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json",
		authorization: "Bearer " + unexpiringJwt,
	}}, requests)
}

func TestApiSendsStdinAsJsonUnlessAContentTypeIsGiven(t *testing.T) {
	for _, tt := range []struct {
		args            []string
		wantContentType string
	}{
		{args: nil, wantContentType: "application/json"},
		{args: []string{"-H", "Content-Type: application/vnd.custom+json"}, wantContentType: "application/vnd.custom+json"},
	} {
		t.Run(tt.wantContentType, func(t *testing.T) {
			var requests []recordedRequest
			endpoint := fakeMeshStack(t, &requests, gohttp.StatusCreated, `{}`)

			_, err := runApi(t, endpoint, `{"spec":{}}`, append([]string{"-X", "POST", "/api/x", "--input", "-"}, tt.args...)...)

			require.NoError(t, err)
			require.Len(t, requests, 1)
			assert.Equal(t, tt.wantContentType, requests[0].contentType)
			assert.JSONEq(t, `{"spec":{}}`, requests[0].body)
		})
	}
}

func TestApiWritesTheBodyOfAFailedAnswerAndFails(t *testing.T) {
	var requests []recordedRequest
	endpoint := fakeMeshStack(t, &requests, gohttp.StatusConflict, `{"message":"still in use"}`)

	stdout, err := runApi(t, endpoint, "", "/api/x")

	require.EqualError(t, err, "meshStack answered HTTP 409")
	assert.JSONEq(t, `{"message":"still in use"}`, stdout)
}

func TestApiSendsNoTokenToAnotherHost(t *testing.T) {
	var requests []recordedRequest
	endpoint := fakeMeshStack(t, &requests, gohttp.StatusOK, `{}`)

	for _, target := range []string{"https://example.com/api/x", "//example.com/api/x"} {
		_, err := runApi(t, endpoint, "", target)

		require.ErrorContains(t, err, "is not a path")
	}
	assert.Empty(t, requests)
}

func fakeMeshStack(t *testing.T, requests *[]recordedRequest, status int, answer string) string {
	t.Helper()
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		*requests = append(*requests, recordedRequest{
			method:        r.Method,
			pathAndQuery:  r.URL.RequestURI(),
			accept:        strings.Join(r.Header["Accept"], ","),
			contentType:   strings.Join(r.Header["Content-Type"], ","),
			authorization: strings.Join(r.Header["Authorization"], ","),
			body:          string(body),
		})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(meshStack.Close)
	return meshStack.URL
}

func runApi(t *testing.T, endpoint string, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
	t.Setenv(setting.Endpoint.EnvKey(), endpoint)
	t.Setenv(setting.SkipVersionCheck.EnvKey(), "true")
	t.Setenv(setting.Profile.EnvKey(), "")
	t.Setenv(setting.Workspace.EnvKey(), "")
	t.Setenv(setting.ApiKeyClientId.EnvKey(), "")
	t.Setenv(setting.ApiKeyClientSecret.EnvKey(), "")
	t.Setenv(setting.ApiToken.EnvKey(), unexpiringJwt)

	var stdout bytes.Buffer
	cmd := api.New()
	// as the root command does
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	err := cmd.ExecuteContext(t.Context())
	return stdout.String(), err
}
