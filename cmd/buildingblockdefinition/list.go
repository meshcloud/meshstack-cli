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
		Long: `List the building block definitions that the workspace owns, and those published to the whole
platform, newest first. A session that works in no workspace lists every definition the credential
can see.`,
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
