package buildingblock

import (
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestRejectRunRejectsOnlyAfterConfirmation(t *testing.T) {
	const waitingRun = "a0000000-0000-4000-8000-000000000003"
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{meshObject(t, `{"metadata": {"uuid": "`+buildingBlockUuid+`"}, "status": {"action": "APPROVE_RUN", "latestRunUuid": "`+waitingRun+`"}, `+
			`"_links": {"latestRun": {"href": "`+runsPath+waitingRun+`"}}}`)},
		BuildingBlockRuns: []any{meshObject(t, `{"metadata": {"uuid": "`+waitingRun+`"}, "status": "IN_PROGRESS", `+
			`"spec": {"runNumber": 2, "behavior": "APPLY", "buildingBlock": {"uuid": "`+buildingBlockUuid+`", "spec": {"displayName": "my-block"}}}, `+
			`"_links": {"reject": {"href": "`+runsPath+waitingRun+`/reject"}}}`)},
	})
	rejections := 0
	rejectAnswer := gohttp.StatusAccepted
	server.Route("POST "+runsPath+waitingRun+"/reject", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		rejections++
		w.WriteHeader(rejectAnswer)
	})

	t.Run("a run that is not confirmed stays waiting", func(t *testing.T) {
		action, asked := newTestRunAction(t, server, "n\n")

		require.NoError(t, action.reject(t.Context()))
		assert.True(t, strings.HasSuffix(asked.String(), "Reject this run? [y/N]: "), asked.String())
		assert.Zero(t, rejections)
	})

	t.Run("a confirmed run is rejected through its reject link", func(t *testing.T) {
		action, _ := newTestRunAction(t, server, "y\n")

		require.NoError(t, action.reject(t.Context()))
		assert.Equal(t, 1, rejections)
	})

	t.Run("a run that no longer waits for approval cannot be rejected", func(t *testing.T) {
		rejectAnswer = gohttp.StatusConflict
		action, _ := newTestRunAction(t, server, "y\n")

		require.EqualError(t, action.reject(t.Context()), "run "+waitingRun+" is no longer waiting for approval")
	})

	t.Run("abort points to reject-run for a run that is waiting for approval", func(t *testing.T) {
		action, _ := newTestRunAction(t, server, "y\n")

		require.ErrorContains(t, action.abort(t.Context()), "Reject it with 'meshstack buildingblock reject-run "+buildingBlockUuid+"'")
	})
}
