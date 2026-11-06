package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

func TestMeshBuildingBlockV2_DeletionSuccessful(t *testing.T) {
	deleteRun := func(action enum.Entry[BuildingBlockAction]) *MeshBuildingBlockV2 {
		return &MeshBuildingBlockV2{
			Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
			Status:   &MeshBuildingBlockV2Status{Action: action, LatestRunUuid: new("delete-run")},
		}
	}
	tests := []struct {
		name     string
		bb       *MeshBuildingBlockV2
		wantDone bool
		wantErr  string
	}{
		{name: "a block that is gone (404, hard deletion or purge) is deleted", bb: nil, wantDone: true},
		{
			name:     "a soft-deleted block is deleted",
			bb:       &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{Lifecycle: MeshBuildingBlockV2Lifecycle{State: BuildingBlockLifecycleStateDeleted}}},
			wantDone: true,
		},
		{name: "a delete run that meshStack has not started yet goes on", bb: deleteRun(BuildingBlockActionPending)},
		{name: "a delete run in progress goes on", bb: deleteRun(BuildingBlockActionInProgress)},
		{name: "a failed delete run fails", bb: deleteRun(BuildingBlockActionReviewErrors), wantErr: "the delete run of building block test-uuid ended with action REVIEW_ERRORS"},
		{name: "an aborted delete run fails", bb: deleteRun(BuildingBlockActionReviewAbortedRun), wantErr: "ended with action REVIEW_ABORTED_RUN"},
		{
			name: "a delete run that could not start fails",
			bb: &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{
				Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("apply-run"), RunStartFailedOn: "2026-10-28T12:00:00Z",
			}},
			wantErr: "ended with action REVIEW_ERRORS",
		},
		{name: "a delete run that waits for approval fails at once", bb: deleteRun(BuildingBlockActionApproveRun), wantErr: "waits with action APPROVE_RUN"},
		{name: "a delete run that waits for a dependency fails at once", bb: deleteRun(BuildingBlockActionWaitingForDependency), wantErr: "waits with action WAITING_FOR_DEPENDENCY"},
		{
			name: "a failed delete run of a force-purged block goes on, as the purge still reaches DELETED",
			bb: &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{
				Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("delete-run"), ForcePurge: true,
			}},
		},
		{
			name:    "a failed delete run of a block without a uuid names it unknown",
			bb:      &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("delete-run")}},
			wantErr: "<unknown>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := tt.bb.DeletionSuccessful()
			assert.Equal(t, tt.wantDone, done)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
