package auth

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newLogout() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove this profile's stored credentials",
		Long: `Remove the stored credentials of a profile.

Without a profile named, it asks which stored profile to log out of, among those for the endpoint.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			currentProfile, _, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
				SettingSources: append(internal.SettingSources(), newProfileSelectionSource(prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr()), false)),
				StoredOnly:     true,
			})
			if errors.Is(err, profile.ErrNoStoredProfile) {
				slog.WarnContext(ctx, "Nothing to log out of, as "+err.Error())
				return nil
			} else if err != nil {
				return err
			}
			if err := currentProfile.RemoveCredentials(ctx); err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("Logged out of profile '%s'", currentProfile))
			return nil
		},
	}
	// TODO add flag --revoke-session to also revoke the login session (refresh token)
	return cmd
}
