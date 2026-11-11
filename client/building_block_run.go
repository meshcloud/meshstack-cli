package client

import (
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/hal"
)

type MeshBuildingBlockRun struct {
	Metadata MeshBuildingBlockRunMetadata `json:"metadata"`
	Spec     MeshBuildingBlockRunSpec     `json:"spec"`
	Status   string                       `json:"status"`
	Links    hal.Links                    `json:"_links,omitzero"`
}

func (run MeshBuildingBlockRun) IsRunning() bool {
	return run.Status == "PENDING" || run.Status == "IN_PROGRESS"
}

type MeshBuildingBlockRunMetadata struct {
	Uuid      string    `json:"uuid"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
}

type MeshBuildingBlockRunSpec struct {
	RunNumber     int                               `json:"runNumber"`
	Behavior      string                            `json:"behavior"`
	BuildingBlock MeshBuildingBlockRunBuildingBlock `json:"buildingBlock"`
}

type MeshBuildingBlockRunBuildingBlock struct {
	Uuid string                                `json:"uuid"`
	Spec MeshBuildingBlockRunBuildingBlockSpec `json:"spec"`
}

type MeshBuildingBlockRunBuildingBlockSpec struct {
	DisplayName            string                       `json:"displayName"`
	TargetRef              MeshBuildingBlockV2TargetRef `json:"targetRef"`
	WorkspaceIdentifier    string                       `json:"workspaceIdentifier"`
	ProjectIdentifier      string                       `json:"projectIdentifier,omitempty"`
	FullPlatformIdentifier string                       `json:"fullPlatformIdentifier,omitempty"`
}

type MeshBuildingBlockRunFilter struct {
	BuildingBlockUuid string `json:"buildingBlockUuid,omitzero"`
}

type MeshBuildingBlockRunClient = MeshObjectClient[MeshBuildingBlockRun, MeshBuildingBlockRunFilter]

// MeshBuildingBlockRunApproval names the plan, because meshStack replaces it when a waiting run is planned again.
type MeshBuildingBlockRunApproval struct {
	PredecessorRunUuid string `json:"predecessorRunUuid"`
}

func (run MeshBuildingBlockRun) meshObjectId() string {
	return run.Metadata.Uuid
}

type MeshBuildingBlockRunLogs struct {
	Steps []MeshBuildingBlockRunStepLog `json:"steps"`
}

type MeshBuildingBlockRunStepLog struct {
	DisplayName   string  `json:"displayName"`
	Status        string  `json:"status"`
	UserMessage   *string `json:"userMessage"`
	SystemMessage *string `json:"systemMessage"`
}
