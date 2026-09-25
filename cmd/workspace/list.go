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
		Short: "List the workspaces visible from the current workspace",
		Long: `List the workspaces visible from the current workspace: --workspace, MESHSTACK_WORKSPACE,
or the profile's default workspace.

From the workspace behind meshPanel's admin area, a role that may list every workspace, such as
Organization Admin, lists them all. From any other workspace the list holds that workspace alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.Listing.Workspaces(ctx)
			})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
