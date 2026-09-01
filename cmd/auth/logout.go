package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func newLogout(in *cli.Input) *cobra.Command {
	var revoke bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove this profile's stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// The profile's own store, because this command acts on that file: otherwise a
			// shell exporting a credential would make logging out a no-op against a memory
			// store while the file stayed where it was.
			selection, err := profile.Select(ctx, in, setting.Environ())
			if err != nil {
				return err
			}
			store, err := profile.NewFileStore(selection.Name)
			if err != nil {
				return err
			}
			// Read from the file rather than from the session, for the same reason: what is
			// about to be removed is what this command reports, whatever the environment
			// would have authenticated with.
			credentials, err := store.Read()
			if err != nil {
				return err
			}
			was := credentials.Current

			session, err := auth.ResolveSession(ctx, auth.ResolveSessionOptions{Settings: in, Store: store})
			if err != nil {
				return err
			}
			if err := session.Logout(ctx, revoke); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Removed the credentials of profile %q for %s.\n", session.Profile, session.Endpoint)
			if revoke {
				_, _ = fmt.Fprintln(out, "The session at the identity provider was ended too.")
				return nil
			}
			if was == credential.MethodLogin {
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
