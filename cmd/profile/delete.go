package profile

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newDelete() *cobra.Command {
	yesFlag := internal.Flag[bool]{Name: "yes", Help: "delete the profile --profile or --endpoint names without asking"}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a profile and its stored credentials",
		Long: `Delete a profile and its stored credentials.

Without --profile, it asks which profile to delete, among those for the endpoint. It asks before it
deletes, unless --yes goes with --profile, or with an --endpoint that only one profile is for.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if yesFlag.Value && !cmd.Flags().Changed(internal.ProfileFlag.Name.String()) && !cmd.Flags().Changed(internal.EndpointFlag.Name.String()) {
				return fmt.Errorf("--%s deletes only the profile --%s or --%s names", yesFlag.Name, internal.ProfileFlag.Name, internal.EndpointFlag.Name)
			}
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			return withLockedProfiles(ctx, func(profiles profile.Profiles) error {
				deleted, named, err := selectProfile(ctx, cmd, p, profiles)
				if err != nil {
					return err
				}
				if !yesFlag.Value || !named {
					confirmed, err := p.Confirm(ctx, fmt.Sprintf("Delete profile '%s' and its stored credentials?", deleted.Name))
					if err != nil {
						return err
					}
					if !confirmed {
						slog.InfoContext(ctx, fmt.Sprintf("Kept profile '%s'.", deleted.Name))
						return nil
					}
				}
				if err := remove(ctx, &profiles, deleted.Name); err != nil {
					return err
				}
				slog.InfoContext(ctx, fmt.Sprintf("Deleted profile '%s'.", deleted.Name))
				return nil
			})
		},
	}
	yesFlag.Register(cmd.Flags())
	return cmd
}
