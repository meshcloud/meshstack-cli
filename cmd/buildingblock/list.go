package buildingblock

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var flags internal.ListFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building blocks, newest first",
		Long: `List the building blocks of the workspace, newest first. A session that works in no workspace
lists every building block the credential can see.`,
		Example: `  meshstack buildingblock list --workspace my-workspace
  meshstack bb list --limit unlimited -o ndjson`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				filter client.MeshBuildingBlockV2ListFilter
				err    error
			)
			if filter.WorkspaceIdentifier, err = internal.ListWorkspace(cmd.Context()); err != nil {
				return err
			}
			return flags.Run[client.MeshBuildingBlockV2](cmd, filter)
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
