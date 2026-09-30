package auth

import (
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

Unless --profile or MESHSTACK_PROFILE names the profile, it asks which of the stored profiles to log
out of, and offers only those for the endpoint where --endpoint or MESHSTACK_ENDPOINT gives one.
Where the input ends before an answer, it logs out of the current profile if that is one of them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			currentProfile, _, err := profile.ResolveProfile(cmd.Context(), profile.ResolveProfileOptions{
				SettingSources: append(internal.SettingSources(), newProfileSelectionSource(prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr()), false)),
			})
			if err != nil {
				return err
			}
			return currentProfile.RemoveCredentials(cmd.Context())
		},
	}
	// TODO add flag --revoke-session to also revoke the login session (refresh token)
	return cmd
}
