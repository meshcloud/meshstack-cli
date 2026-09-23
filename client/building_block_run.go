package client

import (
	"context"
	"encoding/json/jsontext"
	"iter"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type MeshBuildingBlockRun struct {
	Metadata MeshBuildingBlockRunMetadata `json:"metadata"`
	Spec     MeshBuildingBlockRunSpec     `json:"spec"`
	Status   string                       `json:"status"`
}

type MeshBuildingBlockRunMetadata struct {
	Uuid      string `json:"uuid"`
	CreatedOn string `json:"createdOn"`
}

type MeshBuildingBlockRunSpec struct {
	RunNumber int64  `json:"runNumber"`
	Behavior  string `json:"behavior"`
	// BuildingBlock is the block this run belongs to. A listing that spans several blocks says
	// which one a run came from through this field alone.
	BuildingBlock MeshBuildingBlockRunBuildingBlock `json:"buildingBlock"`
}

// MeshBuildingBlockRunBuildingBlock is spec.buildingBlock of a meshBuildingBlockRun. The wire also
// carries spec.buildingBlock.spec.inputs, which holds the run's input values and is left out here
// because a sensitive one goes out encrypted for the runner alone
// (MeshBuildingBlockRunRepresentationModelAssembler.getSpecEnrichedWithInputsFromRun).
type MeshBuildingBlockRunBuildingBlock struct {
	Uuid string                                `json:"uuid"`
	Spec MeshBuildingBlockRunBuildingBlockSpec `json:"spec"`
}

type MeshBuildingBlockRunBuildingBlockSpec struct {
	DisplayName string `json:"displayName"`
	// TargetRef names a meshTenant by uuid or a meshWorkspace by name, the same shape the
	// building block itself carries.
	TargetRef              MeshBuildingBlockV2TargetRef `json:"targetRef"`
	WorkspaceIdentifier    string                       `json:"workspaceIdentifier"`
	ProjectIdentifier      *string                      `json:"projectIdentifier"`
	FullPlatformIdentifier *string                      `json:"fullPlatformIdentifier"`
}

// MeshBuildingBlockRunListFilter holds the filters for the building block run list endpoint. The
// backend requires buildingBlockUuid, because MeshBuildingBlockRunReadController implements no
// plain list of every run.
type MeshBuildingBlockRunListFilter struct {
	BuildingBlockUuid string `json:"buildingBlockUuid"`
}

// MeshBuildingBlockRunLogs is the response from the download-logs actions endpoint.
type MeshBuildingBlockRunLogs struct {
	Steps []MeshBuildingBlockRunStepLog `json:"steps"`
}

// MeshBuildingBlockRunStepLog represents a single step's log data.
type MeshBuildingBlockRunStepLog struct {
	DisplayName   string  `json:"displayName"`
	Status        string  `json:"status"`
	UserMessage   *string `json:"userMessage"`
	SystemMessage *string `json:"systemMessage"`
}

type MeshBuildingBlockRunClient interface {
	GetLogs(ctx context.Context, runUuid string) (MeshBuildingBlockRunLogs, error)
}

type meshBuildingBlockRunClient struct {
	meshObject internal.MeshObjectClient[MeshBuildingBlockRun]
}

func newBuildingBlockRunClient(ctx context.Context, httpClient internal.HttpClient) meshBuildingBlockRunClient {
	return meshBuildingBlockRunClient{
		meshObject: internal.NewMeshObjectClient[MeshBuildingBlockRun](ctx, httpClient, "v1"),
	}
}

func (c meshBuildingBlockRunClient) ListRawSeq(ctx context.Context, filter MeshBuildingBlockRunListFilter) iter.Seq2[jsontext.Value, error] {
	return c.meshObject.ListSeqAs[jsontext.Value](ctx, http.WithUrlQuery(filter))
}

func (c meshBuildingBlockRunClient) GetLogs(ctx context.Context, runUuid string) (MeshBuildingBlockRunLogs, error) {
	return c.meshObject.GetAtPath[MeshBuildingBlockRunLogs](ctx, runUuid, "logs")
}
