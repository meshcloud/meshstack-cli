package client

import (
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

func TestMeshBuildingBlockV2_DeletionSuccessful(t *testing.T) {
	tests := []struct {
		name     string
		bb       *MeshBuildingBlockV2
		wantDone bool
		wantErr  bool
	}{
		{
			name:     "nil (404 — hard deletion / purge)",
			bb:       nil,
			wantDone: true,
			wantErr:  false,
		},
		{
			name: "lifecycle state DELETED (soft delete completed, block still returned)",
			bb: &MeshBuildingBlockV2{
				Status: &MeshBuildingBlockV2Status{
					Lifecycle: MeshBuildingBlockV2Lifecycle{State: BuildingBlockLifecycleStateDeleted},
				},
			},
			wantDone: true,
			wantErr:  false,
		},
		{
			name: "status FAILED during deletion",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status: &MeshBuildingBlockV2Status{
					Status: BuildingBlockStatusFailed,
				},
			},
			wantDone: false,
			wantErr:  true,
		},
		{
			name: "status FAILED but force-purged keeps polling (transient, will reach DELETED)",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status: &MeshBuildingBlockV2Status{
					Status:     BuildingBlockStatusFailed,
					ForcePurge: true,
				},
			},
			wantDone: false,
			wantErr:  false,
		},
		{
			name: "status FAILED with nil Uuid does not panic",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: nil},
				Status: &MeshBuildingBlockV2Status{
					Status: BuildingBlockStatusFailed,
				},
			},
			wantDone: false,
			wantErr:  true,
		},
		{
			name: "still in progress (MARKED_FOR_DELETION lifecycle, non-failed status)",
			bb: &MeshBuildingBlockV2{
				Status: &MeshBuildingBlockV2Status{
					Lifecycle: MeshBuildingBlockV2Lifecycle{State: BuildingBlockLifecycleStateMarkedForDeletion},
				},
			},
			wantDone: false,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := tt.bb.DeletionSuccessful()
			assert.Equal(t, tt.wantDone, done)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMeshBuildingBlockV2_CreateSuccessful(t *testing.T) {
	tests := []struct {
		name        string
		bb          *MeshBuildingBlockV2
		wantDone    bool
		wantErr     bool
		errContains string
	}{
		{
			name:     "nil (not found after creation)",
			bb:       nil,
			wantDone: false,
			wantErr:  true,
		},
		{
			name:     "no status yet — keep polling",
			bb:       &MeshBuildingBlockV2{Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")}},
			wantDone: false,
			wantErr:  false,
		},
		{
			name: "SUCCEEDED",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusSucceeded},
			},
			wantDone: true,
			wantErr:  false,
		},
		{
			name: "FAILED",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusFailed},
			},
			wantDone: false,
			wantErr:  true,
		},
		{
			name: "ABORTED",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusAborted},
			},
			wantDone: false,
			wantErr:  true,
		},
		{
			name: "WAITING_FOR_USER_INPUT — terminal but non-fatal",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusWaitingForUserInput},
			},
			wantDone: true,
			wantErr:  false,
		},
		{
			name: "WAITING_FOR_APPROVAL — terminal but non-fatal",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusWaitingForApproval},
			},
			wantDone: true,
			wantErr:  false,
		},
		{
			name: "FAILED with nil Uuid does not panic",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: nil},
				Status:   &MeshBuildingBlockV2Status{Status: BuildingBlockStatusFailed},
			},
			wantDone:    false,
			wantErr:     true,
			errContains: "<unknown>",
		},
		{
			name: "unknown status — fail fast",
			bb: &MeshBuildingBlockV2{
				Metadata: MeshBuildingBlockV2Metadata{Uuid: new("test-uuid")},
				Status:   &MeshBuildingBlockV2Status{Status: enum.Entry[BuildingBlockStatus]("SOMETHING_NEW")},
			},
			wantDone:    false,
			wantErr:     true,
			errContains: "unknown building block status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, err := tt.bb.CreateSuccessful()
			assert.Equal(t, tt.wantDone, done)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTriggerRunAnswersWithTheBuildingBlockMeshStackAccepted(t *testing.T) {
	var request string
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		request = r.Method + " " + r.URL.Path + " " + string(body)
		w.WriteHeader(gohttp.StatusAccepted)
		_, _ = io.WriteString(w, `{
			"kind": "meshBuildingBlock",
			"apiVersion": "v2-preview",
			"metadata": {"uuid": "87ce02e9-4608-41f2-adf7-3d0aac496d06", "ownedByWorkspace": "my-workspace"},
			"spec": {
				"buildingBlockDefinitionVersionRef": {"uuid": "c02cf622-2d9f-47b1-b42a-dff09b5fe1b2", "kind": "meshBuildingBlockDefinitionVersion"},
				"targetRef": {"uuid": "45f02516-b29e-4df4-b6d2-9d3dbff9ac26", "kind": "meshTenant"},
				"displayName": "My BuildingBlock",
				"inputs": {},
				"parentBuildingBlockRefs": []
			},
			"status": {
				"status": "PENDING",
				"healthStatus": "FAILED",
				"action": "PENDING",
				"outputs": {},
				"latestRunUuid": "e2e00003-0000-4000-8000-000000000003",
				"runStartFailure": "meshStack could not start a run for this Building Block.",
				"runStartFailedOn": "2026-10-01T15:51:39.619819807Z",
				"forcePurge": false,
				"lifecycle": {"state": "ACTIVE"}
			}
		}`)
	}))
	buildingBlocks := newBuildingBlockV2Client(t.Context(), newTestHttpClient(server))

	accepted, err := buildingBlocks.TriggerRun(t.Context(), "87ce02e9-4608-41f2-adf7-3d0aac496d06")
	require.NoError(t, err)

	assert.Equal(t, `POST /api/meshobjects/meshbuildingblocks/87ce02e9-4608-41f2-adf7-3d0aac496d06/trigger-run {"apiVersion":"v2-preview","kind":"meshBuildingBlock","dryRun":false}`, request)
	require.NotNil(t, accepted.Status)
	assert.Equal(t, BuildingBlockStatusPending, accepted.Status.Status)
	assert.Equal(t, new("e2e00003-0000-4000-8000-000000000003"), accepted.Status.LatestRunUuid)
	assert.Equal(t, new("meshStack could not start a run for this Building Block."), accepted.Status.RunStartFailure)
	assert.Equal(t, new("2026-10-01T15:51:39.619819807Z"), accepted.Status.RunStartFailedOn)
}
