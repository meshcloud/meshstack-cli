// Command meshstack is the command line interface (CLI) for meshStack.
package main

import (
	"context"
	"log/slog"
	"os"

	clog "github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/auth"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/pkg/io"
)

func main() {
	if err := internal.RunWith(context.Background(), internal.DefaultTimeout, func(ctx context.Context) error {
		//nolint:contextcheck // the root's PersistentPreRun derives from cmd.Context(), which cobra sets from this ctx
		return newRootCommand().ExecuteContext(ctx)
	}); err != nil {
		// cobra has already written the error to stderr.
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "meshstack",
		Short: "Command line interface for meshStack",
		// RunE has to be set for cobra to render the usage block at all: its help template
		// skips usage while the command is neither runnable nor a parent of subcommands.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Version: internal.Version,
		// A command that fails prints its error, not the whole help text.
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			setupLogging(debug)
			cmd.SetContext(io.WithStderr(cmd.Context(), cmd.ErrOrStderr()))
		},
	}

	persistentFlags := cmd.PersistentFlags()
	persistentFlags.BoolVar(&debug, "debug", false, "log at debug level")
	internal.EndpointFlag.Register(persistentFlags)
	internal.WorkspaceFlag.Register(persistentFlags)
	internal.SkipVersionCheckFlag.Register(persistentFlags)

	cmd.AddCommand(auth.New())
	// `meshstack login` is a shortcut for `meshstack auth login`. Calling the constructor a second
	// time is the only way to get one: cobra's Aliases rename a command inside its own parent.
	cmd.AddCommand(auth.NewLogin())

	return cmd
}

func setupLogging(debug bool) {
	options := clog.Options{
		ReportTimestamp: true,
		Level:           clog.InfoLevel,
	}
	if debug {
		options.Level = clog.DebugLevel
	}
	slog.SetDefault(slog.New(clog.NewWithOptions(os.Stderr, options)))
}
