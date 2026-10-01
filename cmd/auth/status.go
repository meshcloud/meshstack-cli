package auth

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

var statusTemplate = markdown.Parse("status", `{{template "credential" .}}`+"\n")

func newStatus() *cobra.Command {
	var output internal.ShowFlag
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the credential this run authenticates with",
		Long: `Show the credential a command run with the same flags and environment would authenticate with:
where it comes from, who it is for, and until when it lasts. It never shows a secret.

A browser login is not renewed for this, so its token may show as expired, and it is renewed on
next use. An API key reads its own details from meshStack.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			session, err := auth.ResolveSession(ctx, internal.ResolveClientOptions())
			if err != nil {
				return err
			}
			return output.Show(cmd.OutOrStdout(), statusTemplate, session.Status(ctx))
		},
	}
	output.Register(cmd.Flags())
	return cmd
}
