package buildingblockdefinition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var flags internal.ListFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building block definitions, newest first",
		Long: `List building block definitions, newest first.

--workspace lists the definitions that workspace owns. Without it, the list holds those the
credential can see, as the profile's default workspace does not narrow it. Both add the
definitions published to the whole platform.`,
		Example: `  meshstack buildingblockdefinition list
  meshstack bbd list --workspace my-workspace`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspace, err := internal.ListWorkspace(cmd.Context())
			if err != nil {
				return err
			}
			return flags.Run[client.MeshBuildingBlockDefinition](cmd, client.MeshBuildingBlockDefinitionListFilter{
				OwnedByWorkspace:    workspace,
				IncludeAllPublished: true,
			})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
