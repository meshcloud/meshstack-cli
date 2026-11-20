package tfstate

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client"
)

func readBuildingBlock(ctx context.Context, blocks client.MeshBuildingBlockV2Client, buildingBlockUuid string) (client.MeshBuildingBlockV2, error) {
	block, err := blocks.Read(ctx, buildingBlockUuid)
	switch {
	case err != nil:
		return client.MeshBuildingBlockV2{}, err
	case block == nil:
		return client.MeshBuildingBlockV2{}, fmt.Errorf("building block %s not found", buildingBlockUuid)
	}
	return *block, nil
}

// OpenStore opens the state under the building block's own workspace where workspace is empty,
// because that is where the runner stores it.
func OpenStore(ctx context.Context, meshStack client.Client, workspace string, buildingBlockUuid string) (Store, error) {
	if workspace == "" {
		block, err := readBuildingBlock(ctx, meshStack.BuildingBlockV2, buildingBlockUuid)
		if err != nil {
			return Store{}, err
		}
		workspace = block.Metadata.OwnedByWorkspace
	}
	return NewStore(meshStack.HttpClient, meshStack.Endpoint, workspace, buildingBlockUuid), nil
}

func NoRunOf(ctx context.Context, blocks client.MeshBuildingBlockV2Client, buildingBlockUuid string) error {
	block, err := readBuildingBlock(ctx, blocks, buildingBlockUuid)
	if err != nil {
		return err
	}
	switch {
	case block.Status == nil:
	case block.Status.IsRunning():
		return fmt.Errorf("building block %s has a run that is %s and also writes the state", buildingBlockUuid, block.Status.Action)
	case block.Status.Action == client.BuildingBlockActionApproveRun:
		return fmt.Errorf("building block %s has a run that is waiting for approval and writes the state once approved", buildingBlockUuid)
	}
	return nil
}
