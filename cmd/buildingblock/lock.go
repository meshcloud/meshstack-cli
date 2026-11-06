package buildingblock

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newLock() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "lock <building-block-uuid>",
		Short: "Stop meshStack from running a building block",
		Long: `Lock a building block, named by its uuid, so that meshStack starts no further run of it until it is
unlocked with "meshstack buildingblock unlock". A run that is going on goes to its end. A locked
building block has the action UNLOCK_RUNS.

Locking a building block that is locked changes nothing. The command writes the building block.`,
		Example: `  meshstack buildingblock lock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb lock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 -o ndjson | jq -r .status.action`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return changeLock(cmd, buildingBlockUuid, output.Format, client.MeshBuildingBlockV2Client.Lock, "Locked")
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

// changeLock writes the building block as a read after the change, as trigger-run does, so that it
// carries every field meshStack sends.
func changeLock(
	cmd *cobra.Command,
	buildingBlockUuid uuid.UUID,
	format internal.OutputFormat,
	change func(client.MeshBuildingBlockV2Client, context.Context, string) (*client.MeshBuildingBlockV2, error),
	done string,
) error {
	ctx := cmd.Context()
	meshStack, err := internal.ResolveClient(ctx)
	if err != nil {
		return err
	}
	if _, err = change(meshStack.BuildingBlockV2, ctx, buildingBlockUuid.String()); err != nil {
		return err
	}
	raw, err := meshStack.Raw.Get[client.MeshBuildingBlockV2](ctx, buildingBlockUuid)
	if err != nil {
		return err
	}
	var block linkedBuildingBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return err
	}
	if err := format.WriteItem(cmd.OutOrStdout(), raw); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("%s building block %s. It now has the action %s.", done, buildingBlockUuid, block.Status.Action))
	return nil
}
