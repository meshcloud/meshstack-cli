package client_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestRunnerRun(t *testing.T) {
	data, readErr := os.ReadFile("testdata/runner_run.json")
	require.NoError(t, readErr)
	var run client.MeshRunnerRun
	require.NoError(t, json.Unmarshal(data, &run))

	t.Run("round-trips unchanged", func(t *testing.T) {
		assert.Equal(t, "12345678901234567890", run.Spec.BuildingBlock.Spec.Inputs[0].Value.String())
		implementationType, err := run.Spec.Definition.Spec.ImplementationType()
		require.NoError(t, err)
		assert.Equal(t, client.MeshRunnerImplementationTypeTerraform, implementationType)

		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		assert.JSONEq(t, string(data), string(encoded))
	})

	t.Run("round-trips unchanged once decrypted", func(t *testing.T) {
		var err error
		run.Spec.BuildingBlock.Spec.Inputs[3].Value, err = json.Marshal("plaintext")
		require.NoError(t, err)
		var implementation client.MeshRunnerTerraformImplementation
		require.NoError(t, json.Unmarshal(run.Spec.Definition.Spec.Implementation, &implementation))
		implementation.SshPrivateKey = new("-----BEGIN OPENSSH PRIVATE KEY-----")
		run.Spec.Definition.Spec.Implementation, err = json.Marshal(implementation)
		require.NoError(t, err)

		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		var decoded client.MeshRunnerRun
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		reencoded, err := json.Marshal(decoded)
		require.NoError(t, err)
		assert.JSONEq(t, string(encoded), string(reencoded))
		assert.JSONEq(t, `"plaintext"`, decoded.Spec.BuildingBlock.Spec.Inputs[3].Value.String())
	})
}

func TestRunnerRunClient(t *testing.T) {
	t.Run("PatchStatus sends only the fields set, with the run token", func(t *testing.T) {
		sourceId := uuid.MustParse("98520496-627d-43e6-82da-ce499179ff3f")
		runClient := newRunClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			assert.Equal(t, gohttp.MethodPatch, r.Method)
			assert.Equal(t, "/runs/c662eb0d/status/source/"+sourceId.String(), r.URL.Path)
			assert.Equal(t, "Bearer a-run-token", r.Header.Get("Authorization"))
			assert.Equal(t, "application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json", r.Header.Get("Content-Type"))
			assert.Equal(t, fakemeshstack.UserAgent.Version, r.Header.Get("X-Meshcloud-Runner-Version"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{
				"status": "SUCCEEDED",
				"summary": null,
				"steps": [{"id": "manual", "displayName": "Manual Block Run", "status": "SUCCEEDED", "userMessage": "",
					"outputs": {"count": {"value": 12345678901234567890, "type": "INTEGER", "isSensitive": false}}}]
			}`, string(body))
			_, _ = w.Write([]byte(`{"runAborted": true}`))
		})

		result, err := runClient.PatchStatus(t.Context(), sourceId, client.MeshRunnerRunStatusUpdate{
			Status: new("SUCCEEDED"),
			Steps: []client.MeshRunnerRunStepStatusUpdate{{
				Id: "manual", DisplayName: "Manual Block Run", Status: new("SUCCEEDED"), UserMessage: new(""),
				Outputs: map[string]client.MeshRunnerRunOutput{"count": {Value: jsontext.Value("12345678901234567890"), Type: "INTEGER"}},
			}},
		})
		require.NoError(t, err)
		assert.True(t, result.Abort)
	})

	t.Run("DownloadPlanArtifact streams up to the cap", func(t *testing.T) {
		var size atomic.Int64
		size.Store(1 << 20)
		runClient := newRunClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			assert.Equal(t, "/plan", r.URL.Path)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Accept"))
			_, _ = io.CopyN(w, zeros{}, size.Load())
		})

		var plan bytes.Buffer
		require.NoError(t, runClient.DownloadPlanArtifact(t.Context(), &plan))
		assert.Equal(t, size.Load(), int64(plan.Len()))

		size.Store(128<<20 + 1)
		assert.ErrorContains(t, runClient.DownloadPlanArtifact(t.Context(), io.Discard), "the artifact exceeds 134217728 bytes")
	})

	t.Run("UploadArtifact sends the file", func(t *testing.T) {
		planFile := filepath.Join(t.TempDir(), "plan.tfplan")
		require.NoError(t, os.WriteFile(planFile, []byte("a plan"), 0o600))
		runClient := newRunClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			assert.Equal(t, gohttp.MethodPut, r.Method)
			assert.Equal(t, "/upload", r.URL.Path)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
			assert.Equal(t, int64(6), r.ContentLength)
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "a plan", string(body))
			w.WriteHeader(gohttp.StatusNoContent)
		})

		require.NoError(t, runClient.UploadArtifact(t.Context(), planFile))
	})
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func newRunClient(t *testing.T, handler gohttp.HandlerFunc) client.MeshRunnerRunClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := client.New(t.Context(), xurl.MustParsef("%s", server.URL), http.NewClient(fakemeshstack.UserAgent), http.BasicAuth{Username: "runner", Password: "secret"})
	return c.Runner.ForRun(client.MeshRunnerRun{
		Spec: client.MeshRunnerRunSpec{RunToken: "a-run-token"},
		Links: client.MeshRunnerRunLinks{
			UpdateSource:   client.MeshRunnerRunLink{Href: server.URL + "/runs/c662eb0d/status/source/{sourceId}", Templated: true},
			PlanArtifact:   client.MeshRunnerRunLink{Href: server.URL + "/plan"},
			ArtifactUpload: client.MeshRunnerRunLink{Href: server.URL + "/upload"},
		},
	})
}
