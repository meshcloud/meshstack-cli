package client

import (
	"context"
	"encoding/json/jsontext"
	"iter"

	"github.com/meshcloud/meshstack-cli/client/internal"
)

// ListOptions shape how every listing on a context fetches its pages.
type ListOptions = internal.ListOptions

// Page is what a page of a listing says about the listing as a whole.
type Page = internal.Page

func WithListOptions(ctx context.Context, options ListOptions) context.Context {
	return internal.WithListOptions(ctx, options)
}

// ListOptionsFrom returns the options [WithListOptions] put on ctx, and the zero value without.
func ListOptionsFrom(ctx context.Context) ListOptions {
	return internal.ListOptionsFrom(ctx)
}

// MeshListingClient yields meshObjects as the server sent them, which is what a listing prints. It
// is not part of each kind's client interface because the Terraform provider implements those with
// mocks, and a method added there breaks its build.
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
