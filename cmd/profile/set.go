package profile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// settings are the keys `profile set` accepts, in the order the error lists them.
var settings = []string{"endpoint", "workspace"}

// newSet never creates a profile: `meshstack auth login` is the one command that does, so a
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

			// No session: this command writes config.json and never authenticates, so
			// demanding a credential it will not use would refuse to fix a broken profile.
			selection, err := profile.Select(cmd.Context(), in, setting.Environ())
			if err != nil {
				return err
			}

			switch key {
			case "endpoint":
				err = profile.SetEndpoint(selection.Name, value)
			case "workspace":
				err = profile.SetWorkspace(selection.Name, value)
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Profile %q now has %s %s.\n", selection.Name, key, value)
			return nil
		},
	}
}
