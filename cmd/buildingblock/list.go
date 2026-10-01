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

--workspace, or MESHSTACK_WORKSPACE, lists that workspace's building blocks. Without either the
backend lists what the credential can see, whatever default workspace the profile has.`,
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
