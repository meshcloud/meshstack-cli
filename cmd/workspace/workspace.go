// Package workspace holds `meshstack workspace` and its leaves.
package workspace

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
)

func New(in *cli.Input) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Work with meshStack workspaces",
		// Both are needed together: cobra returns flag.ErrHelp for a command that is not
		// runnable before it reaches ValidateArgs, so a parent without RunE would make
		// `meshstack workspace bogus` print help and exit 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList(in))

	return cmd
}
