package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
)

// newLogout builds `meshstack auth logout`. There is no per-method logout: the credentials
// file is the login, and removing it is the whole of it.
func newLogout(in *cli.Input) *cobra.Command {
	var revoke bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove this profile's stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Resolved the way a login is, because this command acts on the profile's file:
			// otherwise a shell that exports a credential would make logging out a no-op
			// against a memory store while the file stayed where it was.
			session, err := auth.ResolveForLogin(ctx, in)
			if err != nil {
				return err
			}
			was := session.Method()
			if err := session.Logout(ctx, revoke); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Removed the credentials of profile %q for %s.\n", session.Profile, session.Endpoint)
			if revoke {
				_, _ = fmt.Fprintln(out, "The session at the identity provider was ended too.")
				return nil
			}
			if was == method.Login {
				// The endpoints that list and revoke CLI logins are on meshStack's internal
				// API, which a CLI token cannot reach, so meshPanel is the other way.
				_, _ = fmt.Fprintln(out, "The session at the identity provider is untouched. End it with --revoke, or in meshPanel under Profile → CLI Logins.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&revoke, "revoke", false, "also end the session at the identity provider")

	return cmd
}
