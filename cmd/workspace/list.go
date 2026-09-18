package workspace

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workspaces this login can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			return internal.WriteList(cmd.OutOrStdout(), output.Format, meshStack.Workspace.ListSeq(ctx))
		},
	}

	output.Register(cmd.Flags())

	return cmd
}
