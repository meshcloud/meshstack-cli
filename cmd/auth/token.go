package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

func newToken() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print an access token for the meshStack API",
		Long: `Print the access token a command run with the same flags and environment would send, renewed
where it is about to expire. Nothing else goes to stdout, so $(meshstack auth token) works.

Paste it as the Bearer token in the API docs to try a request against your meshStack. The token is
short-lived: run this again once the API answers 401. For scripts, meshstack api renews the token by
itself. A browser login prints a token for the workspace this run resolves.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, err := auth.ResolveSession(cmd.Context(), internal.ResolveClientOptions())
			if err != nil {
				return err
			}
			token, err := session.GetBearerToken(cmd.Context())
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
			return err
		},
	}
}
