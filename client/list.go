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
}

type meshListingClient struct {
	workspace        meshWorkspaceClient
	buildingBlockV2  meshBuildingBlockV2Client
	buildingBlockRun meshBuildingBlockRunClient
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
