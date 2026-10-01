package buildingblockdefinitionversion

import (
	"fmt"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		flags          internal.ListFlags
		definitionUuid string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the versions of a building block definition",
		Long: `List the versions of a building block definition.

The definition is named by its uuid, which "meshstack buildingblockdefinition list" reports as
metadata.uuid. It is required: meshStack serves the versions of one definition at a time, and
reading them takes a permission on the workspace that owns it.`,
		Example: `  meshstack buildingblockdefinitionversion list --definition 3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c
  meshstack bbdv list --definition 3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c --limit 1`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := uuid.Parse(definitionUuid); err != nil {
				return fmt.Errorf("--definition %q is no uuid", definitionUuid)
			}
			return flags.Run[client.MeshBuildingBlockDefinitionVersion](cmd, client.MeshBuildingBlockDefinitionVersionListFilter{
				BuildingBlockDefinitionUuid: definitionUuid,
			})
		},
	}

	cmd.Flags().StringVar(&definitionUuid, "definition", "", "list the versions of the building block definition with this uuid")
	_ = cmd.MarkFlagRequired("definition")
	flags.Register(cmd.Flags())

	return cmd
}
