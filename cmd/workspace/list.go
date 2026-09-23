package workspace

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var output internal.OutputFlag

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workspaces this login can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return internal.RunPaged(cmd.Context(), func(ctx context.Context, meshStack client.Client) error {
				return internal.WriteList(cmd.OutOrStdout(), output.Format, meshStack.Workspace.ListRawSeq(ctx))
			})
		},
	}

	output.Register(cmd.Flags())

	return cmd
}
