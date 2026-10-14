package auth

import (
	_ "embed"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

//go:embed status.md.tmpl
var statusTemplateText string

var statusTemplate = markdown.Parse("status", statusTemplateText)

// status adds the meshStack version to the credential, which the profiles show without it.
type status struct {
	auth.CredentialStatus `json:",inline"`

	MeshStackVersion string `json:"meshStackVersion,omitzero"`
}

func showStatus(cmd *cobra.Command, output internal.ShowFlag, session auth.Session) error {
	ctx := cmd.Context()
	shown := status{CredentialStatus: session.Status(ctx)}
	meshInfo, err := session.MeshInfo()
	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cannot read the meshStack version: %s", err))
	}
	shown.MeshStackVersion = meshInfo.Version
	return output.Show(cmd.OutOrStdout(), statusTemplate, shown)
}

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
			session, err := auth.ResolveSession(cmd.Context(), internal.ResolveClientOptions())
			if err != nil {
				return err
			}
			return showStatus(cmd, output, session)
		},
	}
	output.Register(cmd.Flags())
	return cmd
}
