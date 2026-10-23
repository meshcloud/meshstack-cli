package auth

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newLogout() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove this profile's stored credentials",
		Long: `Remove this profile's stored credentials. For a browser login, this also ends the login's
session at meshStack, so its tokens stop working. Your browser stays signed in to meshStack.

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
			if err := auth.Logout(ctx, currentProfile, internal.ResolveClientOptions()); err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("Logged out of profile '%s'", currentProfile))
			return nil
		},
	}
	return cmd
}
