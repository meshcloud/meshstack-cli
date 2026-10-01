package buildingblockdefinition

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
		Short: "List building block definitions",
		Long: `List building block definitions.

--workspace, or MESHSTACK_WORKSPACE, lists the definitions that workspace owns. Without it the
backend lists the ones published across the platform as well.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspace, err := internal.ListWorkspace(cmd.Context())
			if err != nil {
				return err
			}
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.Listing.BuildingBlockDefinitions(ctx, workspace)
			})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
