package buildingblock

import (
	"encoding/json/jsontext"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestAwaitRunStartTakesNothingTheTriggerRunAnswerReportedForTheStart(t *testing.T) {
	const (
		previousRun = "a0000000-0000-4000-8000-000000000001"
		failedOn    = "2026-10-01T10:00:00Z"
	)
	buildingBlockUuid := uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000001")
	server := fakemeshstack.Start(t, fakemeshstack.Options{BuildingBlocks: []any{jsontext.Value(`{"metadata": {"uuid": "` + buildingBlockUuid.String() +
		`"}, "spec": {}, "status": {"status": "PENDING", "latestRunUuid": "` + previousRun +
		`", "runStartFailure": "the previous request failed", "runStartFailedOn": "` + failedOn + `"}}`)}})
	testlogin.LoggedInTo(t, server.URL)
	meshStack, err := internal.ResolveClient(t.Context())
	require.NoError(t, err)

	_, err = awaitRunStart(t.Context(), meshStack.Raw, buildingBlockUuid, &client.MeshBuildingBlockV2Status{
		LatestRunUuid:    new(previousRun),
		RunStartFailure:  new("the previous request failed"),
		RunStartFailedOn: new(failedOn),
	}, time.Millisecond, 50*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "had not started it 50ms later")
	assert.Greater(t, len(server.TakeRequests()), 1, "the building block is read again until the wait is over")
}
