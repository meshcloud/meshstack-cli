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
	const (
		waitingRun         = "a0000000-0000-4000-8000-000000000003"
		inProgressRun      = "a0000000-0000-4000-8000-000000000004"
		finishedRun        = "a0000000-0000-4000-8000-000000000005"
		runOfOtherBlock    = "a0000000-0000-4000-8000-000000000006"
		otherBuildingBlock = "b1d2c3e4-0000-4000-8000-000000000009"
	)
	runWithLinks := func(runUuid, ofBuildingBlock, status string, linkNames ...string) any {
		var links []string
		for _, name := range linkNames {
			links = append(links, `"`+name+`": {"href": "`+runsPath+runUuid+`/`+name+`"}`)
		}
		return meshObject(t, `{"metadata": {"uuid": "`+runUuid+`"}, "status": "`+status+`", `+
			`"spec": {"buildingBlock": {"uuid": "`+ofBuildingBlock+`"}}, "_links": {`+strings.Join(links, ", ")+`}}`)
	}
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{meshObject(t, `{"metadata": {"uuid": "`+buildingBlockUuid+`"}, "status": {"action": "APPROVE_RUN"}}`)},
		BuildingBlockRuns: []any{
			runWithLinks(waitingRun, buildingBlockUuid, "IN_PROGRESS", "approve", "abort"),
			runWithLinks(inProgressRun, buildingBlockUuid, "IN_PROGRESS", "abort"),
			runWithLinks(finishedRun, buildingBlockUuid, "SUCCEEDED"),
			runWithLinks(runOfOtherBlock, otherBuildingBlock, "IN_PROGRESS", "abort"),
		},
	})
	var aborted []string
	for _, runUuid := range []string{waitingRun, inProgressRun, runOfOtherBlock} {
		server.Route("POST "+runsPath+runUuid+"/abort", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			aborted = append(aborted, runUuid)
			w.WriteHeader(gohttp.StatusAccepted)
		})
	}
	testlogin.LoggedInTo(t, server.URL)
	abortRun := func(stdin string, args ...string) error {
		cmd := New()
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"abort-run", buildingBlockUuid}, args...))
		return cmd.ExecuteContext(t.Context())
	}

	t.Run("without a terminal and without --run, nothing is asked and nothing is aborted", func(t *testing.T) {
		require.EqualError(t, abortRun("y\n"), "abort-run needs a terminal, because it asks you to confirm. 'buildingblock abort-run --help' shows how to run it without a terminal")
		assert.Empty(t, server.TakeRequests())
	})

	t.Run("--run aborts a run that waits for approval", func(t *testing.T) {
		require.NoError(t, abortRun("", "--run", waitingRun))
		assert.Equal(t, []string{waitingRun}, aborted)
	})

	t.Run("--run aborts a run that is in progress", func(t *testing.T) {
		require.NoError(t, abortRun("", "--run", inProgressRun))
		assert.Equal(t, []string{waitingRun, inProgressRun}, aborted)
	})

	t.Run("--run does not abort a run that cannot be aborted", func(t *testing.T) {
		require.ErrorContains(t, abortRun("", "--run", finishedRun), "has the status SUCCEEDED and cannot be aborted")
		assert.Equal(t, []string{waitingRun, inProgressRun}, aborted)
	})

	t.Run("--run does not abort a run of another building block", func(t *testing.T) {
		require.ErrorContains(t, abortRun("", "--run", runOfOtherBlock), "belongs to building block "+otherBuildingBlock)
		assert.Equal(t, []string{waitingRun, inProgressRun}, aborted)
	})
}
