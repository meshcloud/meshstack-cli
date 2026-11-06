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
		Short: "Show which credential commands authenticate with",
		Long: `Show which credential other commands with the same flags and environment would authenticate with: where it comes from, who it is for, and how long it is valid. It never shows a secret.

This command does not renew the token of a browser login, so the token may show as expired. The next command that needs the token renews it.`,
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
