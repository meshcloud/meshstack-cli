package buildingblock

import (
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newUnlock() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "unlock <building-block-uuid>",
		Short: "Let meshStack run a locked building block again",
		Long: `Unlock a building block whose status.action is UNLOCK_RUNS. Give the building block by its uuid. meshStack starts no run of a locked building block until it is unlocked. An update with skipRunOn locks a building block, and so does 'meshstack buildingblock lock'.

After the unlock, meshStack decides whether the building block runs, as after an update, and approval policies apply. Unlocking a building block that is not locked changes nothing. The command prints the building block.`,
		Example: `  meshstack buildingblock unlock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb unlock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 -o ndjson | jq -r .status.action`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return changeLock(cmd, buildingBlockUuid, output.Format, client.MeshBuildingBlockV2Client.Unlock, "Unlocked")
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}
