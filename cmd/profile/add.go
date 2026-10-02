package profile

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newAdd() *cobra.Command {
	workspaceFlag := defaultWorkspaceFlag()
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a profile",
		Long: `Add a profile, in a form on a terminal and line by line otherwise. --profile, --endpoint and
--workspace give the default answers, which an empty answer takes, as does an input that ends.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			return withLockedProfiles(ctx, func(profiles profile.Profiles) error {
				return edit(ctx, p, profiles, draft{
					name:      internal.ProfileFlag.Value,
					endpoint:  internal.EndpointFlag.Value,
					workspace: workspaceFlag.Value,
				})
			})
		},
	}
	workspaceFlag.Register(cmd.Flags())
	return cmd
}
