package buildingblock_test

import (
	"bytes"
	"encoding/json/v2"
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

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
	buildingBlockPath = "/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid
	triggerRunPath    = buildingBlockPath + "/trigger-run"
	previousRunUuid   = "a0000000-0000-4000-8000-000000000001"
	startedRunUuid    = "a0000000-0000-4000-8000-000000000002"
)

func buildingBlockWithStatus(status string) string {
	return fmt.Sprintf(`{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, "status": %s}`, buildingBlockUuid, status)
}

// fakeMeshStack answers trigger-run with accepted, and every read of the building block with read.
func fakeMeshStack(t *testing.T, accepted, read string) *[]string {
	t.Helper()
	var requests []string
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests = append(requests, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == gohttp.MethodPost && r.URL.Path == triggerRunPath:
			w.WriteHeader(gohttp.StatusAccepted)
			_, _ = io.WriteString(w, accepted)
		case r.Method == gohttp.MethodGet && r.URL.Path == buildingBlockPath:
			_, _ = io.WriteString(w, read)
		default:
			gohttp.NotFound(w, r)
		}
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)
	return &requests
}

func TestTriggerRunWritesTheBuildingBlockOnceMeshStackStartedTheRun(t *testing.T) {
	requests := fakeMeshStack(t,
		buildingBlockWithStatus(`{"status": "PENDING", "latestRunUuid": "`+previousRunUuid+`"}`),
		buildingBlockWithStatus(`{"status": "PENDING", "latestRunUuid": "`+startedRunUuid+`"}`))

	cmd := buildingblock.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	require.Len(t, *requests, 2, "the command triggers the run and reads the building block until the run started, and does not wait for it to finish")
	assert.Contains(t, (*requests)[0], "POST "+triggerRunPath)
	assert.Contains(t, (*requests)[0], `"dryRun":false`)
	assert.Contains(t, (*requests)[1], "GET "+buildingBlockPath)
	var written struct {
		Status struct {
			LatestRunUuid string `json:"latestRunUuid"`
		} `json:"status"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &written))
	assert.Equal(t, startedRunUuid, written.Status.LatestRunUuid)
}

func TestTriggerRunFailsWithTheReasonMeshStackCouldNotStartTheRun(t *testing.T) {
	const reason = "meshStack could not start a run for this Building Block.\n\nAsk your platform team to look up the details in the meshStack logs."
	failure := func(failedOn string) string {
		return buildingBlockWithStatus(fmt.Sprintf(`{"status": "PENDING", "latestRunUuid": %q, "runStartFailure": %q, "runStartFailedOn": %q}`,
			previousRunUuid, reason, failedOn))
	}
	fakeMeshStack(t, failure("2026-10-01T10:00:00Z"), failure("2026-10-01T10:05:00Z"))

	cmd := buildingblock.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})

	require.EqualError(t, cmd.ExecuteContext(t.Context()), reason)
	assert.Contains(t, stdout.String(), `"runStartFailedOn": "2026-10-01T10:05:00Z"`, "the building block still goes to stdout, for a script to read the failure from")
}

func TestTriggerRunRejectsANameOfNoUuidBeforeItAsksMeshStack(t *testing.T) {
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(gohttp.ResponseWriter, *gohttp.Request) {
		t.Error("the command asked meshStack about a building block of no uuid")
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	cmd.SetArgs([]string{"trigger-run", "my-building-block"})

	assert.EqualError(t, cmd.ExecuteContext(t.Context()), `invalid argument "my-building-block": invalid uuid`)
}
