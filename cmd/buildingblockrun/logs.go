package buildingblockrun

import (
	"fmt"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newLogs() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "logs <run-uuid>",
		Short: "Show the logs of a building block run",
		Long: `Show the logs of a building block run.

The run is named by its uuid, which "meshstack buildingblockrun list" reports as metadata.uuid.
Each step of the run carries its own status and messages: systemMessage holds what the runner
produced, userMessage what the run reported back to the user.`,
		Example: `  meshstack buildingblockrun logs 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c
  meshstack bbrun logs 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c -o ndjson`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runUuid, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("%q is no uuid", args[0])
			}
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			logs, err := meshStack.Raw.Get[client.MeshBuildingBlockRun](ctx, runUuid, "logs")
			if err != nil {
				return err
			}
			return internal.WriteItem(cmd.OutOrStdout(), output.Format, logs)
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}
