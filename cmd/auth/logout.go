package auth

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

func newLogout() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove this profile's stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			currentProfile, err := profile.ResolveProfile(cmd.Context(), profile.ResolveProfileOptions{
				ExplicitSourcesOption: internal.ExplicitSourcesOption(),
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
