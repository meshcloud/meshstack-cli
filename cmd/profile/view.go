package profile

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

func newView(in *cli.Input) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Show the resolved configuration and where each value came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolved the way an ordinary command is, so that what this prints is what the
			// next command will use, including a credential that never reaches a profile.
			session, err := auth.ResolveSession(cmd.Context(), auth.ResolveSessionOptions{Settings: in})
			if err != nil {
				return err
			}
			status, err := session.Status()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Profile", status.Profile)
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Endpoint", status.Endpoint)
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Workspace", orNone(status.Workspace))
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Method", status.Current.Description())

			// In resolution order rather than sorted: that is the order the precedence rules
			// were applied in, and the order a person reading them wants.
			_, _ = fmt.Fprintln(out, "\nSources")
			for _, origin := range status.Origins {
				_, _ = fmt.Fprintf(out, "  %-20s %s\n", origin.Key, origin.Source)
			}

			_, _ = fmt.Fprintln(out, "\nFiles")
			_, _ = fmt.Fprintf(out, "  %-11s %s\n", "config", status.ConfigPath)
			_, _ = fmt.Fprintf(out, "  %-11s %s\n", "credentials", status.CredentialsPath)
			return nil
		},
	}
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
