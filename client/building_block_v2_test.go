package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

func TestMeshBuildingBlockV2Status_RunChangedSince(t *testing.T) {
	const (
		failedOn      = "2026-10-28T12:00:00Z"
		laterFailedOn = "2026-10-28T13:00:00Z"
	)
	idle := MeshBuildingBlockV2Status{Action: BuildingBlockActionNoAction, LatestRunUuid: new("run-1")}
	with := func(base MeshBuildingBlockV2Status, change func(*MeshBuildingBlockV2Status)) MeshBuildingBlockV2Status {
		change(&base)
		return base
	}
	tests := []struct {
		name   string
		before MeshBuildingBlockV2Status
		now    MeshBuildingBlockV2Status
		want   bool
	}{
		{"a block without a status before has changed", MeshBuildingBlockV2Status{}, idle, true},
		{"a created block that waits for its first run has changed against a zero before", MeshBuildingBlockV2Status{}, MeshBuildingBlockV2Status{Action: BuildingBlockActionPending}, true},
		{"the same status has not changed", idle, idle, false},
		{"a new latest run has changed", idle, with(idle, func(s *MeshBuildingBlockV2Status) { s.LatestRunUuid = new("run-2") }), true},
		{"a new latest dry run has changed", idle, with(idle, func(s *MeshBuildingBlockV2Status) { s.LatestDryRunUuid = new("dry-run-1") }), true},
		{"a first run start failure has changed", idle, with(idle, func(s *MeshBuildingBlockV2Status) {
			s.Action, s.RunStartFailedOn = BuildingBlockActionReviewErrors, failedOn
		}), true},
		{
			"a newer run start failure has changed",
			with(idle, func(s *MeshBuildingBlockV2Status) { s.RunStartFailedOn = failedOn }),
			with(idle, func(s *MeshBuildingBlockV2Status) { s.RunStartFailedOn = laterFailedOn }), true,
		},
		{
			"a cleared run start failure alone has not changed",
			with(idle, func(s *MeshBuildingBlockV2Status) {
				s.Action, s.RunStartFailedOn = BuildingBlockActionReviewErrors, failedOn
			}),
			idle, false,
		},
		{
			"a cleared run start failure with a new run has changed",
			with(idle, func(s *MeshBuildingBlockV2Status) {
				s.Action, s.RunStartFailedOn = BuildingBlockActionReviewErrors, failedOn
			}),
			with(idle, func(s *MeshBuildingBlockV2Status) {
				s.Action, s.LatestRunUuid = BuildingBlockActionPending, new("run-2")
			}), true,
		},
		{
			"a new action of a block that was not running has changed, as supplied input resumes the same run",
			with(idle, func(s *MeshBuildingBlockV2Status) { s.Action = BuildingBlockActionProvideInput }),
			with(idle, func(s *MeshBuildingBlockV2Status) { s.Action = BuildingBlockActionPending }), true,
		},
		{
			"a new action of a block that was running has not changed, as it belongs to the run before",
			with(idle, func(s *MeshBuildingBlockV2Status) { s.Action = BuildingBlockActionInProgress }),
			with(idle, func(s *MeshBuildingBlockV2Status) { s.Action = BuildingBlockActionReviewErrors }), false,
		},
		{
			"UPGRADE_AVAILABLE reads as NO_ACTION, as a released definition version starts no run",
			idle, with(idle, func(s *MeshBuildingBlockV2Status) { s.Action = BuildingBlockActionUpgradeAvailable }), false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.now.RunChangedSince(tt.before))
		})
	}
}

func TestMeshBuildingBlockV2_RunEndedSince(t *testing.T) {
	before := MeshBuildingBlockV2Status{Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("run-1")}
	block := func(action enum.Entry[BuildingBlockAction], latestRunUuid string) *MeshBuildingBlockV2 {
		return &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{Action: action, LatestRunUuid: new(latestRunUuid)}}
	}
	tests := []struct {
		name     string
		bb       *MeshBuildingBlockV2
		wantDone bool
	}{
		{"a block without a status goes on", &MeshBuildingBlockV2{}, false},
		{"the outcome of the run before goes on", block(BuildingBlockActionReviewErrors, "run-1"), false},
		{"a pending run goes on", block(BuildingBlockActionPending, "run-2"), false},
		{"a run in progress goes on", block(BuildingBlockActionInProgress, "run-2"), false},
		{"a run that needs no action ends", block(BuildingBlockActionNoAction, "run-2"), true},
		{"a failed run ends without an error", block(BuildingBlockActionReviewErrors, "run-2"), true},
		{"a run that waits for approval ends", block(BuildingBlockActionApproveRun, "run-2"), true},
		{"an action this client does not know ends without an error", block(enum.Entry[BuildingBlockAction]("SOMETHING_NEW"), "run-2"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := tt.bb.RunEndedSince(before)
			assert.Equal(t, tt.wantDone, done)
			assert.NoError(t, err)
		})
	}

	t.Run("a block that is gone fails", func(t *testing.T) {
		_, err := (*MeshBuildingBlockV2)(nil).RunEndedSince(MeshBuildingBlockV2Status{})
		require.Error(t, err)
	})
}

func TestMeshBuildingBlockV2_DeletionSuccessful(t *testing.T) {
	before := MeshBuildingBlockV2Status{Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("apply-run")}
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
		{
			name: "REVIEW_ERRORS of the run before the deletion goes on",
			bb:   &MeshBuildingBlockV2{Status: &MeshBuildingBlockV2Status{Action: BuildingBlockActionReviewErrors, LatestRunUuid: new("apply-run")}},
		},
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
			done, err := tt.bb.DeletionSuccessful(before)
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
