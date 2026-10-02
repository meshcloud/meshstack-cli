package workspace_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/workspace"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestAListCutShortByTheLimitSaysHowManyThereAre(t *testing.T) {
	var workspaces []any
	for i := range 6 {
		workspaces = append(workspaces, map[string]any{"metadata": map[string]string{"name": fmt.Sprintf("workspace-%d", i)}})
	}
	server := fakemeshstack.Start(t, fakemeshstack.Options{Workspaces: workspaces, PageSize: 2})
	testlogin.LoggedInTo(t, server.URL)
	captured := logs.Capture(t)
	var stdout bytes.Buffer
	cmd := workspace.New()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "--limit", "1"})

	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.JSONEq(t, `[{"metadata":{"name":"workspace-0"}}]`, stdout.String())
	assert.Contains(t, captured.String(), "listed the first 1 of 6")
	assert.Len(t, server.TakeRequests(), 1)
}
