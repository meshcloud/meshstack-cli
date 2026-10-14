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

From the workspace behind meshPanel's admin area, a role that may list every workspace, such as
Organization Admin, lists them all. From any other workspace the list holds that workspace alone.`,
		Example: `  meshstack workspace list
  meshstack ws list --workspace admin-workspace --limit unlimited -o ndjson`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run[client.MeshWorkspace](cmd, client.MeshWorkspaceListFilter{})
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
