package buildingblockdefinitionversion

import (
	"context"
	"encoding/json/jsontext"
	"iter"

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
metadata.uuid. It is required: the backend serves the versions of one definition at a time, and
reading them takes a permission on the workspace that owns it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.Listing.BuildingBlockDefinitionVersions(ctx, definitionUuid)
			})
		},
	}

	cmd.Flags().StringVar(&definitionUuid, "definition", "", "list the versions of the building block definition with this uuid")
	_ = cmd.MarkFlagRequired("definition")
	flags.Register(cmd.Flags())

	return cmd
}
