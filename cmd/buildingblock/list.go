package buildingblock

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building blocks",
		Long: `List building blocks.

--workspace lists that workspace's building blocks. Without it the backend lists what the
credential can see.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var filter client.MeshBuildingBlockV2ListFilter
			if workspace := internal.WorkspaceFlag.Value; workspace != "" {
				filter.WorkspaceIdentifier = &workspace
			}
			return internal.RunPaged(cmd.Context(), func(ctx context.Context, meshStack client.Client) error {
				return internal.WriteList(cmd.OutOrStdout(), output.Format, meshStack.BuildingBlockV2.ListRawSeq(ctx, filter))
			})
		},
	}

	output.Register(cmd.Flags())

	return cmd
}
