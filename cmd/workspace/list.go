package workspace

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
		Short: "List the workspaces this login can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.Listing.Workspaces(ctx)
			})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
