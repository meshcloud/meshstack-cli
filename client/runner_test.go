package client_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"math/big"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestRunnerRun(t *testing.T) {
	data, readErr := os.ReadFile("testdata/runner_run.json")
	require.NoError(t, readErr)
	var run client.MeshRunnerRun
	require.NoError(t, json.Unmarshal(data, &run))

	t.Run("decodes the inputs by type and round-trips unchanged", func(t *testing.T) {
		inputs := run.Spec.BuildingBlock.Spec.Inputs
		count, isBig := inputs[0].Value.(*big.Int)
		require.True(t, isBig)
		assert.Equal(t, "12345678901234567890", count.String())
		assert.InDelta(t, 0.5, inputs[1].Value, 0)
		assert.Equal(t, map[string]any{"nested": []any{1.0, 2.0}}, inputs[2].Value)
		assert.Equal(t, "ZW5jcnlwdGVk", inputs[3].Value)
		assert.Equal(t, "eu", inputs[4].Value)

		implementation := run.Spec.Definition.Spec.Implementation
		assert.Equal(t, client.MeshRunnerImplementationTypeTerraform, implementation.Type)
		require.NotNil(t, implementation.Terraform)
		assert.Equal(t, "1.9.0", implementation.Terraform.TerraformVersion)
		assert.True(t, implementation.Terraform.Async)

		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		assert.JSONEq(t, string(data), string(encoded))
	})

	t.Run("round-trips unchanged once decrypted", func(t *testing.T) {
		run.Spec.BuildingBlock.Spec.Inputs[3].Value = "plaintext"
		run.Spec.Definition.Spec.Implementation.Terraform.SshPrivateKey = new("-----BEGIN OPENSSH PRIVATE KEY-----")

		encoded, err := json.Marshal(run)
		require.NoError(t, err)
		var decoded client.MeshRunnerRun
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		reencoded, err := json.Marshal(decoded)
		require.NoError(t, err)
		assert.JSONEq(t, string(encoded), string(reencoded))
		assert.Equal(t, "plaintext", decoded.Spec.BuildingBlock.Spec.Inputs[3].Value)
	})

	t.Run("an implementation keeps its type", func(t *testing.T) {
		for _, implementation := range []string{
			`{"type":"MANUAL"}`,
			`{"type":"GITLAB_CICD","gitlabBaseUrl":"https://gitlab.com","projectId":"1","refName":"main","pipelineTriggerToken":"token"}`,
			`{"type":"A_NEW_TYPE"}`,
		} {
			var decoded client.MeshRunnerRunImplementation
			require.NoError(t, json.Unmarshal([]byte(implementation), &decoded))
			encoded, err := json.Marshal(decoded)
			require.NoError(t, err)
			assert.JSONEq(t, implementation, string(encoded))
		}
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

	t.Run("DownloadArtifact streams up to the cap", func(t *testing.T) {
		var size atomic.Int64
		size.Store(1 << 20)
		runClient := newRunClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
			assert.Equal(t, "/artifact", r.URL.Path)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Accept"))
			_, _ = io.CopyN(w, zeros{}, size.Load())
		})

		var plan bytes.Buffer
		require.NoError(t, runClient.DownloadArtifact(t.Context(), &plan))
		assert.Equal(t, size.Load(), int64(plan.Len()))

		size.Store(128<<20 + 1)
		assert.ErrorContains(t, runClient.DownloadArtifact(t.Context(), io.Discard), "the artifact exceeds 134217728 bytes")
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
	c := client.New(t.Context(), xurl.MustParsef("%s", server.URL), http.NewClient(fakemeshstack.UserAgent), http.BearerToken("runner-token"))
	return c.Runner.ForRun(client.MeshRunnerRun{
		Spec: client.MeshRunnerRunSpec{RunToken: "a-run-token"},
		Links: client.MeshRunnerRunLinks{
			UpdateSource: client.MeshRunnerRunLink{Href: server.URL + "/runs/c662eb0d/status/source/{sourceId}", Templated: true},
			Artifact:     client.MeshRunnerRunLink{Href: server.URL + "/artifact"},
		},
	})
}
