package buildingblockrun

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newLogs() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "logs <run-uuid>",
		Short: "Show the logs of a building block run (experimental)",
		Long: `Show the logs of a building block run.

Experimental: the output shape may still change.

The run is named by its uuid, which "meshstack buildingblockrun list" reports as metadata.uuid.
Each step of the run carries its own status and messages: systemMessage holds what the runner
produced, userMessage what the run reported back to the user.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			logs, err := meshStack.BuildingBlockRun.GetLogs(ctx, args[0])
			if err != nil {
				return err
			}
			return internal.WriteItem(cmd.OutOrStdout(), output.Format, logs)
		},
	}

	output.Register(cmd.Flags())

	return cmd
}
