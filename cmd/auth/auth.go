// Package auth holds `meshstack auth` and its leaves. It is also where the two render
// helpers below live, because every table this package prints has the same shape and a
// helper file would not be a leaf command.
package auth

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/internal/cli"
)

func New(in *cli.Input) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication with meshStack",
		// Both are needed together. Cobra returns flag.ErrHelp for a command that is
		// not runnable, before it reaches ValidateArgs, so dropping RunE would make
		// `meshstack auth bogus` print help and exit 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewLogin(in))
	cmd.AddCommand(newStatus(in))
	cmd.AddCommand(newLogout(in))

	return cmd
}

// row prints one label, its value, and an optional parenthetical saying where the value
// came from. Fixed columns rather than a tabwriter: every table here has the same two
// columns, and a column that never moves stays greppable from a script.
func row(out io.Writer, label, value, detail string) {
	if detail == "" {
		_, _ = fmt.Fprintf(out, "%-9s %s\n", label, value)
		return
	}
	_, _ = fmt.Fprintf(out, "%-9s %-28s (%s)\n", label, value, detail)
}

// humanDuration renders a deadline the way a person reads one, and says "ago" rather than
// printing a negative number.
func humanDuration(d time.Duration) string {
	if d < 0 {
		return d.Abs().Round(time.Second).String() + " ago"
	}
	return "in " + d.Round(time.Second).String()
}
