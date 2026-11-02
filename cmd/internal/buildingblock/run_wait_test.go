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
	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestRunWait(t *testing.T) {
	buildingBlockUuid := uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000003")
	before := client.MeshBuildingBlockV2Status{Action: client.BuildingBlockActionNoAction, LatestRunUuid: new("run-1")}
	var status atomic.Pointer[string]
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{})
	meshStack.Route("GET /api/meshobjects/meshbuildingblocks/{uuid}", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = fmt.Fprintf(w, `{"metadata": {"uuid": %q}, "spec": {}, "status": {%s}, "_links": {"meshPanel": {"href": "https://panel.example/block"}}}`,
			buildingBlockUuid, *status.Load())
	})
	raw := client.New(t.Context(), xurl.MustParsef("%s", meshStack.URL), http.NewClient(fakemeshstack.UserAgent), http.BearerToken(fakemeshstack.Token)).Raw
	wait := RunWait{raw: raw, buildingBlockUuid: buildingBlockUuid, startEvery: time.Millisecond, startWithin: 50 * time.Millisecond}

	t.Run("a preflight run that a trigger with an approval policy starts is a reaction", func(t *testing.T) {
		status.Store(new(`"action": "IN_PROGRESS", "latestRunUuid": "run-1", "latestDryRunUuid": "preflight-1"`))

		_, block, err := wait.AwaitReaction(t.Context(), before)

		require.NoError(t, err)
		assert.Equal(t, "preflight-1", block.Status.GetLatestDryRunUuid())
	})

	t.Run("fails when meshStack does not react within the start window", func(t *testing.T) {
		status.Store(new(`"action": "NO_ACTION", "latestRunUuid": "run-1"`))

		_, _, err := wait.AwaitReaction(t.Context(), before)

		require.ErrorContains(t, err, "meshStack did not react to the write of building block "+buildingBlockUuid.String()+" within 50ms")
		require.ErrorContains(t, err, "https://panel.example/block")
	})

	t.Run("fails with the reason meshStack could not start the run", func(t *testing.T) {
		status.Store(new(`"action": "REVIEW_ERRORS", "latestRunUuid": "run-1", "runStartFailure": "no runner", "runStartFailedOn": "2026-10-29T10:00:00Z"`))

		_, block, err := wait.AwaitReaction(t.Context(), before)
		require.NoError(t, err)

		require.ErrorContains(t, wait.StartFailure(block), "meshStack could not start the run of building block "+buildingBlockUuid.String()+": no runner")
	})
}
