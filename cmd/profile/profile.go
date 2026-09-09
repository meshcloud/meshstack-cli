// Package profile holds `meshstack profile` and its leaves.
//
// `meshstack profile list` and `meshstack profile use <name>` are the shape to grow into.
// They are not built: --profile and MESHSTACK_PROFILE already select one per invocation,
// and `profile view` covers the rest.
package profile

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
)

func New(in *cli.Input) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Inspect and change the configuration profile",
		// Both are needed together: cobra returns flag.ErrHelp for a command that is not
		// runnable before it reaches ValidateArgs, so a parent without RunE would make
		// `meshstack profile bogus` print help and exit 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newView(in))
	cmd.AddCommand(newSet(in))

	return cmd
}
