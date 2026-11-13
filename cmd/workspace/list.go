package workspace

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var flags internal.ListFlags

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workspaces visible from the current workspace, newest first",
		Long: `List the workspaces visible from the current workspace, newest first.

If you work in the admin workspace and have a role that can list all workspaces, such as Organization Admin, the command lists all workspaces. Otherwise it lists only the current workspace.`,
		Example: `  meshstack workspace list
  meshstack ws list --workspace admin-workspace --limit unlimited -o ndjson`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			meshStack, err := internal.ResolveClient(cmd.Context())
			if err != nil {
				return err
			}
			return flags.Run(cmd, meshStack.Workspace, client.NoFilter{})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
