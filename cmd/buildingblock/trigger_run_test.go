package buildingblock_test

import (
	"bytes"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
	triggerRunPath    = "/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid + "/trigger-run"
	// unexpiringJwt is an unsigned JWT that expires in 2100, so the CLI sends it rather than asking for a new one.
	unexpiringJwt = "eyJhbGciOiJub25lIn0.eyJleHAiOjQxMDI0NDQ4MDB9."
)

func TestTriggerRunSendsDryRunAndWritesTheAnsweredBuildingBlock(t *testing.T) {
	for _, tt := range []struct {
		args     []string
		wantBody string
	}{
		{args: nil, wantBody: `"dryRun":false`},
		{args: []string{"--dry-run"}, wantBody: `"dryRun":true`},
	} {
		t.Run(tt.wantBody, func(t *testing.T) {
			var requests []string
			meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				requests = append(requests, r.Method+" "+r.URL.Path+" "+string(body))
				if r.Method != gohttp.MethodPost || r.URL.Path != triggerRunPath {
					gohttp.NotFound(w, r)
					return
				}
				w.WriteHeader(gohttp.StatusAccepted)
				_, _ = io.WriteString(w, `{"metadata":{"uuid":"`+buildingBlockUuid+`"},"status":{"status":"PENDING","latestRunUuid":"r1"}}`)
			}))
			t.Cleanup(meshStack.Close)
			useFakeMeshStack(t, meshStack.URL)

			var stdout bytes.Buffer
			cmd := buildingblock.New()
			cmd.SetArgs(append([]string{"trigger-run", buildingBlockUuid, "-o", "ndjson"}, tt.args...))
			cmd.SetOut(&stdout)
			require.NoError(t, cmd.ExecuteContext(t.Context()))

			require.Len(t, requests, 1, "the command triggers the run and does nothing else, such as waiting for it")
			assert.Contains(t, requests[0], "POST "+triggerRunPath)
			assert.Contains(t, requests[0], tt.wantBody)
			assert.Contains(t, stdout.String(), `"latestRunUuid":"r1"`)
		})
	}
}

func useFakeMeshStack(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
	t.Setenv(setting.Endpoint.EnvKey(), endpoint)
	t.Setenv(setting.SkipVersionCheck.EnvKey(), "true")
	t.Setenv(setting.Profile.EnvKey(), "")
	t.Setenv(setting.Workspace.EnvKey(), "")
	t.Setenv(setting.ApiKeyClientId.EnvKey(), "")
	t.Setenv(setting.ApiKeyClientSecret.EnvKey(), "")
	t.Setenv(setting.ApiToken.EnvKey(), unexpiringJwt)
}
