package buildingblock

import (
	"io"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestAbortRunWithoutATerminalAbortsTheRunItIsGiven(t *testing.T) {
	const staleRun = "a0000000-0000-4000-8000-000000000003"
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{meshObject(t, `{"metadata": {"uuid": "`+buildingBlockUuid+`"}, "status": {"action": "APPROVE_RUN"}}`)},
		BuildingBlockRuns: []any{meshObject(t, `{"metadata": {"uuid": "`+staleRun+`"}, "status": "IN_PROGRESS", `+
			`"spec": {"buildingBlock": {"uuid": "`+buildingBlockUuid+`"}}, "_links": {"abort": {"href": "`+runsPath+staleRun+`/abort"}}}`)},
	})
	aborted := 0
	server.Route("POST "+runsPath+staleRun+"/abort", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		aborted++
		w.WriteHeader(gohttp.StatusAccepted)
	})
	testlogin.LoggedInTo(t, server.URL)
	cmd := New()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"abort-run", buildingBlockUuid, "--run", staleRun})

	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.Equal(t, 1, aborted)
}
