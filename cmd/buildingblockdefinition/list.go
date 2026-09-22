package buildingblockdefinition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building block definitions (experimental)",
		Long: `List building block definitions.

Experimental: the output shape may still change.

--workspace lists the definitions that workspace owns. Without it the backend lists the ones
published across the platform as well.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			var workspace *string
			if identifier := internal.WorkspaceFlag.Value; identifier != "" {
				workspace = &identifier
			}
			return internal.WriteList(cmd.OutOrStdout(), output.Format, meshStack.BuildingBlockDefinition.ListSeq(ctx, workspace))
		},
	}

	output.Register(cmd.Flags())

	return cmd
}
