package tfstate

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/meshbuildingblock/v2"
)

type buildingBlock struct {
	Metadata meshbuildingblock.Metadata        `json:"metadata"`
	Status   *meshbuildingblock.StatusResponse `json:"status"`
}

func readBuildingBlock(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID) (block buildingBlock, err error) {
	read, err := raw.Get[meshbuildingblock.Response](ctx, buildingBlockUuid)
	if err != nil {
		return block, err
	}
	return block, json.Unmarshal(read, &block)
}

func (block buildingBlock) hasUnfinishedRun() bool {
	return block.Status != nil &&
		//nolint:staticcheck // SA1019: moving to status.action is a change of its own, which this switch of types leaves out
		(block.Status.Status == meshbuildingblock.StatusValueInProgress || block.Status.Status == meshbuildingblock.StatusValuePending)
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
	if block.hasUnfinishedRun() {
		return fmt.Errorf("building block %s has a run %s, which writes the state as well", buildingBlockUuid, block.Status.Status) //nolint:staticcheck // SA1019: as in hasUnfinishedRun
	}
	return nil
}
