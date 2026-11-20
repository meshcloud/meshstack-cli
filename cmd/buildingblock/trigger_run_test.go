package buildingblock_test

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

// meshStack reports a failed run start only when it cannot start a run, and no acceptance test can
// bring that about on purpose.
func TestTriggerRunPrintsTheBuildingBlockAndFailsWithTheReasonMeshStackCouldNotStartTheRun(t *testing.T) {
	const (
		buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
		reason            = "meshStack could not start a run for this Building Block.\n\nAsk your platform team to look up the details in the meshStack logs."
	)
	failure := fmt.Sprintf(`{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, `+
		`"status": {"action": "REVIEW_ERRORS", "latestRunUuid": "a0000000-0000-4000-8000-000000000001", "runStartFailure": %q, "runStartFailedOn": "2026-10-01T10:05:00Z"}}`,
		buildingBlockUuid, reason)
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{BuildingBlocks: []any{jsontext.Value(failure)}})
	meshStack.Route("POST /api/meshobjects/meshbuildingblocks/{uuid}/trigger-run", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"metadata": {"uuid": %q}, "spec": {}, "status": {"action": "PENDING", "latestRunUuid": "a0000000-0000-4000-8000-000000000001"}}`, buildingBlockUuid)
	})
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})

	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "meshStack could not start the run of building block "+buildingBlockUuid+": "+reason)
	assert.Contains(t, stdout.String(), `"runStartFailedOn": "2026-10-01T10:05:00Z"`, "the building block still goes to stdout, for a script to read the failure from")
}
