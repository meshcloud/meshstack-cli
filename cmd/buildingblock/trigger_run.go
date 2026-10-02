package buildingblock

import (
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newTriggerRun() *cobra.Command {
	return &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block, named by its uuid, which "meshstack buildingblock list"
reports as metadata.uuid.

The command returns once meshStack has accepted the run, and does not wait for it to finish.
"meshstack buildingblockrun list --building-block <uuid>" follows the run.`,
		Example: `  meshstack buildingblock trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 && meshstack bbrun list --building-block 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --limit 1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The backend answers a uuid it does not know with 404, which says nothing about a typo.
			if _, err := uuid.Parse(args[0]); err != nil {
				return fmt.Errorf("%q is no uuid", args[0])
			}
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			buildingBlockUuid := args[0]
			if _, err := meshStack.BuildingBlockV2.TriggerRun(ctx, buildingBlockUuid); err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("meshStack accepted a run of building block %s. Follow it with `meshstack buildingblockrun list --building-block %s`",
				buildingBlockUuid, buildingBlockUuid))
			return nil
		},
	}
}
