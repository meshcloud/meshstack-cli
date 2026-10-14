package buildingblock_test

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"io"
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
	failure := func(failedOn string) string {
		return fmt.Sprintf(`{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, `+
			`"status": {"status": "PENDING", "latestRunUuid": "a0000000-0000-4000-8000-000000000001", "runStartFailure": %q, "runStartFailedOn": %q}}`,
			buildingBlockUuid, reason, failedOn)
	}
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{BuildingBlocks: []any{jsontext.Value(failure("2026-10-01T10:05:00Z"))}})
	meshStack.Route("POST /api/meshobjects/meshbuildingblocks/{uuid}/trigger-run", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.WriteHeader(gohttp.StatusAccepted)
		_, _ = io.WriteString(w, failure("2026-10-01T10:00:00Z"))
	})
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})

	require.EqualError(t, cmd.ExecuteContext(t.Context()), reason)
	assert.Contains(t, stdout.String(), `"runStartFailedOn": "2026-10-01T10:05:00Z"`, "the building block still goes to stdout, for a script to read the failure from")
}
