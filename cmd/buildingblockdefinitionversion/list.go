package buildingblockdefinitionversion

import (
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		flags          internal.ListFlags
		definitionUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the versions of a building block definition",
		Long: `List the versions of a building block definition.

Give the definition by its uuid; "meshstack buildingblockdefinition list" shows it as
metadata.uuid. --definition is required, because meshStack returns the versions of one definition
at a time. Reading them needs a permission in the workspace that owns the definition.`,
		Example: `  meshstack buildingblockdefinitionversion list --definition 3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c
  meshstack bbdv list --definition 3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c --limit 1`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run[client.MeshBuildingBlockDefinitionVersion](cmd, client.MeshBuildingBlockDefinitionVersionListFilter{
				BuildingBlockDefinitionUuid: definitionUuid,
			})
		},
	}

	cmd.Flags().Var((*internal.UuidFlag)(&definitionUuid), "definition", "list the versions of the building block definition with this uuid")
	_ = cmd.MarkFlagRequired("definition")
	flags.Register(cmd.Flags())

	return cmd
}
