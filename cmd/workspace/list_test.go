package workspace_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/workspace"
)

func TestAListCutShortByTheLimitSaysHowManyThereAre(t *testing.T) {
	requests := 0
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		requests++
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `
		{
			"_embedded": {
				"meshWorkspaces": [
					{"metadata": {"name": "workspace-%[1]d-a"}},
					{"metadata": {"name": "workspace-%[1]d-b"}}
				]
			},
			"page": {"size": 2, "totalElements": 6, "totalPages": 3, "number": %[1]d}
		}`, page)
	}))
	t.Cleanup(server.Close)
	loggedInTo(t, server.URL)
	logs := capturedLogs(t)
	var stdout bytes.Buffer
	cmd := workspace.New()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "--limit", "1"})

	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.JSONEq(t, `[{"metadata":{"name":"workspace-0-a"}}]`, stdout.String())
	assert.Contains(t, logs.String(), "listed the first 1 of 6")
	assert.Equal(t, 1, requests)
}

// loggedInTo points the CLI at endpoint with a token that never expires: one without an expiry
// counts as expired, and would be refreshed before the first request.
func loggedInTo(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("MESHSTACK_ENDPOINT", endpoint)
	t.Setenv("MESHSTACK_API_TOKEN", "e30."+base64.RawURLEncoding.EncodeToString([]byte(`{"exp":4102444800}`))+".signature")
	t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "true")
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
}

func capturedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}
