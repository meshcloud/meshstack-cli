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
		Long: `List building blocks, newest first.

--workspace lists that workspace's building blocks. Without it, the list holds every building
block the credential can see, as the profile's default workspace does not narrow it.`,
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
