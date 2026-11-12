package client

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/hal"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

const buildingBlockRunApiVersion = "v1"

// MeshBuildingBlockRun keeps apiVersion and kind, because a runner passes the run on as it came.
type MeshBuildingBlockRun struct {
	ApiVersion string                       `json:"apiVersion"`
	Kind       string                       `json:"kind"`
	Metadata   MeshBuildingBlockRunMetadata `json:"metadata"`
	Spec       MeshBuildingBlockRunSpec     `json:"spec"`
	Status     string                       `json:"status"`
	Links      hal.Links                    `json:"_links,omitzero"`
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
	Definition    MeshBuildingBlockRunDefinition    `json:"buildingBlockDefinition"`
	RunToken      string                            `json:"runToken"`
	Trigger       *MeshBuildingBlockRunTrigger      `json:"trigger,omitzero"`
}

type MeshBuildingBlockRunBuildingBlock struct {
	Uuid string                                `json:"uuid"`
	Spec MeshBuildingBlockRunBuildingBlockSpec `json:"spec"`
}

type MeshBuildingBlockRunBuildingBlockSpec struct {
	DisplayName            string                            `json:"displayName"`
	TargetRef              MeshBuildingBlockV2TargetRef      `json:"targetRef"`
	WorkspaceIdentifier    string                            `json:"workspaceIdentifier"`
	ProjectIdentifier      string                            `json:"projectIdentifier,omitempty"`
	FullPlatformIdentifier string                            `json:"fullPlatformIdentifier,omitempty"`
	Inputs                 []MeshBuildingBlockRunInput       `json:"inputs"`
	ParentBuildingBlocks   []MeshBuildingBlockRunParentBlock `json:"parentBuildingBlocks"`
}

type MeshBuildingBlockRunParentBlock struct {
	BuildingBlockUuid string `json:"buildingBlockUuid"`
	DefinitionUuid    string `json:"definitionUuid"`
}

type MeshBuildingBlockRunInput struct {
	Key string `json:"key"`
	// Value holds an int64 for an INTEGER and a []string for a MULTI_SELECT.
	Value       any                                 `json:"value"`
	Type        enum.Entry[MeshBuildingBlockIOType] `json:"type"`
	IsSensitive bool                                `json:"isSensitive"`
	Env         bool                                `json:"isEnvironment"`
}

func (i *MeshBuildingBlockRunInput) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type wire MeshBuildingBlockRunInput
	var target struct {
		wire

		Value jsontext.Value `json:"value"`
	}
	if err := json.UnmarshalDecode(dec, &target, dec.Options()); err != nil {
		return err
	}
	*i = MeshBuildingBlockRunInput(target.wire)
	switch i.Type {
	case MeshBuildingBlockIOTypeInteger:
		i.Value = int64(0)
	case MeshBuildingBlockIOTypeMultiSelect:
		i.Value = []string(nil)
	}
	if err := json.Unmarshal(target.Value, &i.Value, dec.Options()); err != nil {
		return fmt.Errorf("input %s: %w", i.Key, err)
	}
	return nil
}

type MeshBuildingBlockRunDefinition struct {
	Uuid string                             `json:"uuid"`
	Spec MeshBuildingBlockRunDefinitionSpec `json:"spec"`
}

type MeshBuildingBlockRunDefinitionSpec struct {
	WorkspaceIdentifier string                             `json:"workspaceIdentifier"`
	Version             int                                `json:"version"`
	Implementation      MeshBuildingBlockRunImplementation `json:"implementation"`
}

type MeshBuildingBlockRunTrigger struct {
	Reason string           `json:"reason"`
	Author MeshObjectAuthor `json:"author"`
}

type MeshBuildingBlockRunFilter struct {
	BuildingBlockUuid string `json:"buildingBlockUuid,omitzero"`
}

var ErrNoRunToClaim = errors.New("no run to claim")

type MeshBuildingBlockRunClient interface {
	MeshObjectClient[MeshBuildingBlockRun, MeshBuildingBlockRunFilter]

	Claim(ctx context.Context, runnerUuid string) (MeshBuildingBlockRun, error)
	// RegisterSource takes a 409 as success: the source was registered before.
	RegisterSource(ctx context.Context, runUuid string, registration MeshBuildingBlockRunSourceRegistration) error
	UpdateSourceSteps(ctx context.Context, runUuid, sourceId string, update MeshBuildingBlockRunStatusUpdate) (MeshBuildingBlockRunStatusUpdateResult, error)
	// DownloadArtifact writes the artifact of the run that this run applies.
	DownloadArtifact(ctx context.Context, runUuid string, w io.Writer) error
	UploadArtifact(ctx context.Context, runUuid, path string) error
}

type meshBuildingBlockRunClient struct {
	meshObjectClient[MeshBuildingBlockRun, MeshBuildingBlockRunFilter]
}

func (c meshBuildingBlockRunClient) Claim(ctx context.Context, runnerUuid string) (MeshBuildingBlockRun, error) {
	run, err := c.unmarshalJsonBody[MeshBuildingBlockRun](c.do(ctx, http.MethodPost, c.collection.JoinPath("create"),
		http.WithUrlQuery(map[string]string{"forRunnerUuid": runnerUuid}),
		http.WithContentType(c.mediaType),
		http.WithHeader("X-Meshcloud-Runner-Version", c.client.UserAgentVersion()),
		http.WithRedactedJsonPointers("/spec/runToken")))
	switch {
	case isNotFound(err):
		return run, ErrNoRunToClaim
	case err == nil && run.Metadata.Uuid == "":
		return run, errors.New("the claimed run has no uuid")
	}
	return run, err
}

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
