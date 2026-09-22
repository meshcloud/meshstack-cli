package client

import (
	"context"
	"encoding/json/jsontext"
	"iter"

	"github.com/meshcloud/meshstack-cli/client/internal"
)

type ListOptions = internal.ListOptions

type Page = internal.Page

func WithListOptions(ctx context.Context, options ListOptions) context.Context {
	return internal.WithListOptions(ctx, options)
}

func ListOptionsFrom(ctx context.Context) ListOptions {
	return internal.ListOptionsFrom(ctx)
}

type MeshListingClient interface {
	Workspaces(ctx context.Context) iter.Seq2[jsontext.Value, error]
	BuildingBlocksV2(ctx context.Context, filter MeshBuildingBlockV2ListFilter) iter.Seq2[jsontext.Value, error]
	BuildingBlockRuns(ctx context.Context, filter MeshBuildingBlockRunListFilter) iter.Seq2[jsontext.Value, error]
	BuildingBlockRunLogs(ctx context.Context, runUuid string) (jsontext.Value, error)
	BuildingBlockDefinitions(ctx context.Context, workspaceIdentifier *string) iter.Seq2[jsontext.Value, error]
	BuildingBlockDefinitionVersions(ctx context.Context, buildingBlockDefinitionUuid string) iter.Seq2[jsontext.Value, error]
}

type meshListingClient struct {
	workspace                      meshWorkspaceClient
	buildingBlockV2                meshBuildingBlockV2Client
	buildingBlockRun               meshBuildingBlockRunClient
	buildingBlockDefinition        meshBuildingBlockDefinitionClient
	buildingBlockDefinitionVersion meshBuildingBlockDefinitionVersionClient
}

func (c meshListingClient) Workspaces(ctx context.Context) iter.Seq2[jsontext.Value, error] {
	return c.workspace.ListRawSeq(ctx)
}

func (c meshListingClient) BuildingBlocksV2(ctx context.Context, filter MeshBuildingBlockV2ListFilter) iter.Seq2[jsontext.Value, error] {
	return c.buildingBlockV2.ListRawSeq(ctx, filter)
}

func (c meshListingClient) BuildingBlockRuns(ctx context.Context, filter MeshBuildingBlockRunListFilter) iter.Seq2[jsontext.Value, error] {
	return c.buildingBlockRun.ListRawSeq(ctx, filter)
}

func (c meshListingClient) BuildingBlockDefinitions(ctx context.Context, workspaceIdentifier *string) iter.Seq2[jsontext.Value, error] {
	return c.buildingBlockDefinition.ListRawSeq(ctx, workspaceIdentifier)
}

func (c meshListingClient) BuildingBlockDefinitionVersions(ctx context.Context, buildingBlockDefinitionUuid string) iter.Seq2[jsontext.Value, error] {
	return c.buildingBlockDefinitionVersion.ListRawSeq(ctx, buildingBlockDefinitionUuid)
}

func (c meshListingClient) BuildingBlockRunLogs(ctx context.Context, runUuid string) (jsontext.Value, error) {
	return c.buildingBlockRun.GetLogsRaw(ctx, runUuid)
}

// MeshRunTriggerClient is a client of the CLI alone: it stays off [MeshBuildingBlockV2Client], whose
// every method the Terraform provider's mocks implement.
type MeshRunTriggerClient interface {
	TriggerBuildingBlockRun(ctx context.Context, buildingBlockUuid string, request MeshBuildingBlockV2TriggerRunRequest) (jsontext.Value, error)
}

type meshRunTriggerClient struct {
	buildingBlockV2 meshBuildingBlockV2Client
}

func (c meshRunTriggerClient) TriggerBuildingBlockRun(ctx context.Context, buildingBlockUuid string, request MeshBuildingBlockV2TriggerRunRequest) (jsontext.Value, error) {
	return c.buildingBlockV2.TriggerRunWith(ctx, buildingBlockUuid, request)
}
