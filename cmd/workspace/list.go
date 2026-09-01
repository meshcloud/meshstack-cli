package workspace

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

// newList builds `meshstack workspace list`. It is one of the two commands that do not
// require a resolved workspace: an unscoped user token reaches this call and almost nothing
// else, which is exactly what makes it the way to find out which workspace to use.
func newList(in *cli.Input) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspaces this credential can see",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			session, err := auth.ResolveSession(ctx, auth.ResolveSessionOptions{Settings: in})
			if err != nil {
				return err
			}
			// Session.Workspaces already annotates its own failures through auth.HintErr.
			names, err := session.Workspaces(ctx)
			if err != nil {
				return err
			}
			if len(names) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "This credential can see no workspaces.")
				return nil
			}
			for _, name := range names {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}
