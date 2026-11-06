package buildingblock

import (
	"fmt"
	gohttp "net/http"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestRunWait(t *testing.T) {
	buildingBlockUuid := uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000003")
	var statuses atomic.Pointer[[]string]
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{})
	meshStack.Route("GET /api/meshobjects/meshbuildingblocks/{uuid}", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		remaining := *statuses.Load()
		if len(remaining) > 1 {
			statuses.Store(new(remaining[1:]))
		}
		_, _ = fmt.Fprintf(w, `{"metadata": {"uuid": %q}, "spec": {}, "status": {%s}, "_links": {"meshPanel": {"href": "https://panel.example/block"}}}`,
			buildingBlockUuid, remaining[0])
	})
	raw := client.New(t.Context(), xurl.MustParsef("%s", meshStack.URL), http.NewClient(fakemeshstack.UserAgent), http.BearerToken(fakemeshstack.Token)).Raw
	wait := RunWait{raw: raw, buildingBlockUuid: buildingBlockUuid, pendingEvery: time.Millisecond, inProgressEvery: time.Millisecond}
	const (
		pending    = `"action": "PENDING", "latestRunUuid": "run-1"`
		inProgress = `"action": "IN_PROGRESS", "latestRunUuid": "run-2"`
	)

	t.Run("the start ends with the first action after PENDING", func(t *testing.T) {
		statuses.Store(&[]string{pending, pending, inProgress, `"action": "NO_ACTION", "latestRunUuid": "run-2"`})

		_, block, err := wait.AwaitStart(t.Context())

		require.NoError(t, err)
		assert.Equal(t, client.BuildingBlockActionInProgress, block.Status.Action)
	})

	t.Run("the end waits out PENDING and IN_PROGRESS", func(t *testing.T) {
		statuses.Store(&[]string{pending, inProgress, inProgress, `"action": "REVIEW_ERRORS", "latestRunUuid": "run-2"`})

		_, block, err := wait.AwaitEnd(t.Context())

		require.NoError(t, err)
		require.ErrorContains(t, wait.Outcome(block), "ended with action REVIEW_ERRORS")
	})

	t.Run("fails with the reason meshStack could not start the run", func(t *testing.T) {
		statuses.Store(&[]string{pending, `"action": "REVIEW_ERRORS", "latestRunUuid": "run-1", "runStartFailure": "no runner", "runStartFailedOn": "2026-10-29T10:00:00Z"`})

		_, block, err := wait.AwaitStart(t.Context())
		require.NoError(t, err)

		require.ErrorContains(t, wait.StartFailure(block), "meshStack could not start the run of building block "+buildingBlockUuid.String()+": no runner")
		require.ErrorContains(t, wait.StartFailure(block), "https://panel.example/block")
	})
}
