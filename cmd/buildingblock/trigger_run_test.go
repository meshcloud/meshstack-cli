package buildingblock_test

import (
	"bytes"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

// meshStack reports a failed run start only when it cannot start a run, and no acceptance test can
// bring that about on purpose.
func TestTriggerRunPrintsTheBuildingBlockAndFailsWithTheReasonMeshStackCouldNotStartTheRun(t *testing.T) {
	const (
		buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
		buildingBlockPath = "/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid
		reason            = "meshStack could not start a run for this Building Block.\n\nAsk your platform team to look up the details in the meshStack logs."
	)
	failure := func(failedOn string) string {
		return fmt.Sprintf(`{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, `+
			`"status": {"status": "PENDING", "latestRunUuid": "a0000000-0000-4000-8000-000000000001", "runStartFailure": %q, "runStartFailedOn": %q}}`,
			buildingBlockUuid, reason, failedOn)
	}
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		switch {
		case r.Method == gohttp.MethodPost && r.URL.Path == buildingBlockPath+"/trigger-run":
			w.WriteHeader(gohttp.StatusAccepted)
			_, _ = io.WriteString(w, failure("2026-10-01T10:00:00Z"))
		case r.Method == gohttp.MethodGet && r.URL.Path == buildingBlockPath:
			_, _ = io.WriteString(w, failure("2026-10-01T10:05:00Z"))
		default:
			gohttp.NotFound(w, r)
		}
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})

	require.EqualError(t, cmd.ExecuteContext(t.Context()), reason)
	assert.Contains(t, stdout.String(), `"runStartFailedOn": "2026-10-01T10:05:00Z"`, "the building block still goes to stdout, for a script to read the failure from")
}
