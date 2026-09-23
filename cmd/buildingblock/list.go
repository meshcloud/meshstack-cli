package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"iter"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var flags internal.ListFlags

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
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.BuildingBlockV2.ListRawSeq(ctx, filter)
			})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
