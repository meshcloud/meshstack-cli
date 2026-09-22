package buildingblock

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newTriggerRun() *cobra.Command {
	var (
		output internal.OutputFlag
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block, and write the building block meshStack answers with.

--dry-run asks for a dry (DETECT) run, which plans the block without changing it.

The command does not wait for the run. meshStack starts it asynchronously, and holds it until a
person approves it in meshPanel if the definition asks for approval. The status.latestRunUuid and
status.latestDryRunUuid of the answer may therefore still name an older run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			buildingBlockUuid := args[0]
			buildingBlock, err := meshStack.BuildingBlockV2.TriggerRunWith(ctx, buildingBlockUuid, client.MeshBuildingBlockV2TriggerRunRequest{DryRun: dryRun})
			if err != nil {
				return err
			}
			if err := internal.WriteItem(cmd.OutOrStdout(), output.Format, buildingBlock); err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("meshStack accepted a run of building block %s. Follow it with `meshstack buildingblockrun list --building-block %s`",
				buildingBlockUuid, buildingBlockUuid))
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "ask for a dry (DETECT) run that changes nothing")
	output.Register(cmd.Flags())

	return cmd
}
