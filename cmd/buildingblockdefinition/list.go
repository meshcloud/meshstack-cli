package buildingblockdefinition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const includeAllPublishedFlagName = "include-all-published"

func newList() *cobra.Command {
	var flags internal.ListFlags
	var includeAllPublished bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building block definitions, newest first",
		Long:  "List building block definitions, newest first. By default, the command lists the definitions of the workspace you work in and those published to the whole platform. Use --" + includeAllPublishedFlagName + "=false to list only the definitions of the workspace you work in. Use --" + internal.WorkspaceFlag.Name.String() + " to list only the definitions that the workspace you name owns. An Organization Admin gets every definition unless they use --" + internal.WorkspaceFlag.Name.String() + ".",
		Example: `  meshstack buildingblockdefinition list
  meshstack bbd list --include-all-published=false
  meshstack bbd list --workspace my-workspace`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return list(cmd, &flags, includeAllPublished)
		},
	}

	flags.Register(cmd.Flags())
	cmd.Flags().BoolVar(&includeAllPublished, includeAllPublishedFlagName, true, "also list the definitions published to the whole platform")

	return cmd
}

func list(cmd *cobra.Command, flags *internal.ListFlags, includeAllPublished bool) error {
	var ownedByWorkspace *string
	if cmd.Flags().Changed(internal.WorkspaceFlag.Name.String()) {
		ownedByWorkspace = new(internal.WorkspaceFlag.Value)
	}
	return flags.Run[client.MeshBuildingBlockDefinition](cmd, client.MeshBuildingBlockDefinitionListFilter{
		OwnedByWorkspace:    ownedByWorkspace,
		IncludeAllPublished: includeAllPublished,
	})
}
