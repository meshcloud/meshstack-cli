package tfstate

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
)

type buildingBlock struct {
	Metadata client.MeshBuildingBlockV2Metadata `json:"metadata"`
	Status   *client.MeshBuildingBlockV2Status  `json:"status"`
}

func readBuildingBlock(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID) (block buildingBlock, err error) {
	read, err := raw.Get[client.MeshBuildingBlockV2](ctx, buildingBlockUuid)
	if err != nil {
		return block, err
	}
	return block, json.Unmarshal(read, &block)
}

// OpenStore opens the state under the building block's own workspace where workspace is empty,
// because that is where the runner stores it.
func OpenStore(ctx context.Context, raw *client.RawClient, workspace string, buildingBlockUuid uuid.UUID) (Store, error) {
	if workspace == "" {
		block, err := readBuildingBlock(ctx, raw, buildingBlockUuid)
		if err != nil {
			return Store{}, err
		}
		workspace = block.Metadata.OwnedByWorkspace
	}
	return NewStore(raw, workspace, buildingBlockUuid), nil
}

func NoRunOf(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID) error {
	block, err := readBuildingBlock(ctx, raw, buildingBlockUuid)
	if err != nil {
		return err
	}
	switch {
	case block.Status == nil:
	case block.Status.IsRunning():
		return fmt.Errorf("building block %s has a run %s, which writes the state as well", buildingBlockUuid, block.Status.Action)
	case block.Status.Action == client.BuildingBlockActionApproveRun:
		return fmt.Errorf("building block %s has a run that waits for approval, which writes the state once approved", buildingBlockUuid)
	}
	return nil
}
