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
		Long: `Print the access token that other commands with the same flags and environment would send. The command renews the token first if it is about to expire. The command prints nothing else to stdout, so $(meshstack auth token) works.

Paste the token as the Bearer token in the API docs to try a request against your meshStack. The token is short-lived: run this command again once the API returns 401. In scripts, use 'meshstack api', which renews the token by itself. For a browser login, the token is for the current workspace.`,
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
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(token))
			return err
		},
	}
}
