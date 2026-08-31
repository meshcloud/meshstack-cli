package profile

import (
	"fmt"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

// newView builds `meshstack profile view`: the resolved configuration, where each value
// came from, and the two files it was read out of. It makes no network call.
func newView(in *cli.Input) *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Show the resolved configuration and where each value came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolved the way an ordinary command is, so that what this prints is what the
			// next command will use, including a credential that never reaches a profile.
			session, err := auth.Resolve(cmd.Context(), in)
			if err != nil {
				return err
			}
			status, err := session.Status()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			profileName := status.Profile
			if profileName == "" {
				profileName = "none"
			}
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Profile", profileName)
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Endpoint", status.Endpoint)
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Workspace", orNone(status.Workspace))
			_, _ = fmt.Fprintf(out, "%-11s %s\n", "Method", status.Current.Description())

			_, _ = fmt.Fprintln(out, "\nSources")
			for _, key := range slices.Sorted(maps.Keys(status.Sources)) {
				_, _ = fmt.Fprintf(out, "  %-11s %s\n", key, orNone(status.Sources[key]))
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
