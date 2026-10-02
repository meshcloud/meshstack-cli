package workspace_test

import (
	"bytes"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/workspace"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
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
	testlogin.LoggedInTo(t, server.URL)
	captured := logs.Capture(t)
	var stdout bytes.Buffer
	cmd := workspace.New()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "--limit", "1"})

	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.JSONEq(t, `[{"metadata":{"name":"workspace-0-a"}}]`, stdout.String())
	assert.Contains(t, captured.String(), "listed the first 1 of 6")
	assert.Equal(t, 1, requests)
}
