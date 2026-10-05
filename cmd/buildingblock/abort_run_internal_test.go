package buildingblock

import (
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestAbortRunAbortsTheNewestRun(t *testing.T) {
	const (
		latestRun = "a0000000-0000-4000-8000-000000000001"
		dryRun    = "a0000000-0000-4000-8000-000000000002"
	)
	runOf := func(runUuid, behavior string) map[string]any {
		return meshObject(t, `{"metadata": {"uuid": "`+runUuid+`"}, "status": "IN_PROGRESS", `+
			`"spec": {"runNumber": 1, "behavior": "`+behavior+`", "buildingBlock": {"uuid": "`+buildingBlockUuid+`", "spec": {"displayName": "my-block"}}}, `+
			`"_links": {"abort": {"href": "`+runsPath+runUuid+`/abort"}}}`)
	}
	block := meshObject(t, `{"metadata": {"uuid": "`+buildingBlockUuid+`"}, "status": {"status": "IN_PROGRESS"}, `+
		`"_links": {"latestRun": {"href": "`+runsPath+latestRun+`"}, "latestDryRun": {"href": "`+runsPath+dryRun+`"}}}`)
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks:    []any{block},
		BuildingBlockRuns: []any{runOf(latestRun, "APPLY"), runOf(dryRun, "DETECT")},
	})
	var aborted []string
	server.Route("POST "+runsPath+"{uuid}/abort", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		aborted = append(aborted, r.URL.Path)
		w.WriteHeader(gohttp.StatusAccepted)
	})

	t.Run("a dry run that is newer than the latest run is the one aborted", func(t *testing.T) {
		action, asked := newTestRunAction(t, server, "y\n")

		require.NoError(t, action.abort(t.Context()))

		assert.Equal(t, []string{runsPath + dryRun + "/abort"}, aborted)
		assert.Contains(t, asked.String(), "is a DETECT run with the status IN_PROGRESS")
	})

	t.Run("without a newer dry run, the latest run is the one aborted", func(t *testing.T) {
		removeLink(t, block, "latestDryRun")
		action, _ := newTestRunAction(t, server, "y\n")

		require.NoError(t, action.abort(t.Context()))

		assert.Equal(t, runsPath+latestRun+"/abort", aborted[len(aborted)-1])
	})
}
