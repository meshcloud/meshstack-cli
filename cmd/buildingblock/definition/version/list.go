package version

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		output         internal.OutputFlag
		definitionUuid string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the versions of a building block definition (experimental)",
		Long: `List the versions of a building block definition.

Experimental: the output shape may still change.

The definition is named by its uuid, which "meshstack buildingblock definition list" reports as
metadata.uuid. It is required: the backend serves the versions of one definition at a time, and
reading them takes a permission on the workspace that owns it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			versions := meshStack.BuildingBlockDefinitionVersion.ListSeq(ctx, definitionUuid)
			return internal.WriteList(cmd.OutOrStdout(), output.Format, versions)
		},
	}

	cmd.Flags().StringVar(&definitionUuid, "definition", "", "list the versions of the building block definition with this uuid")
	_ = cmd.MarkFlagRequired("definition")
	output.Register(cmd.Flags())

	return cmd
}
