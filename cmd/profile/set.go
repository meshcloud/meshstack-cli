package profile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// settings are the keys `profile set` accepts, in the order the error lists them.
var settings = []string{"endpoint", "workspace"}

// newSet builds `meshstack profile set <key> <value>`, which writes to config.json. It
// never creates a profile: `meshstack auth login` is the one command that does, so a
// mistyped name is reported rather than quietly configured.
func newSet(in *cli.Input) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set this profile's endpoint or default workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			// Checked before anything is resolved, so that a typo answers with the list of
			// keys rather than with whatever the configuration happens to be missing.
			if !slices.Contains(settings, key) {
				return diags.Errorf("unknown profile setting",
					"%q is not a profile setting. The settings are: %s.", key, strings.Join(settings, ", "))
			}

			// Resolved the way a login is, because this command configures a profile: a
			// shell that exports a credential must not make it write nothing.
			session, err := auth.ResolveForLogin(cmd.Context(), in)
			if err != nil {
				return err
			}

			switch key {
			case "endpoint":
				err = auth.SetProfileEndpoint(session.Profile, value)
			case "workspace":
				err = auth.SetProfileWorkspace(session.Profile, value)
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Profile %q now has %s %s.\n", session.Profile, key, value)
			return nil
		},
	}
}
