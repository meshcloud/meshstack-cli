// Command meshstack is the command line interface (CLI) for meshStack.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/auth"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func main() {
	if err := run(); err != nil {
		// cobra has already written the error to stderr.
		os.Exit(1)
	}
}

func run() error {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()
	// Ends a run waiting for what never comes, such as a browser login nobody answers.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return newRootCommand().ExecuteContext(ctx)
}

func newRootCommand() *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "meshstack",
		Short: "Command line interface for meshStack",
		// Running `meshstack` on its own prints the help text. RunE also has to be set
		// for cobra to render the usage block at all: its help template skips usage
		// while the command is neither runnable nor a parent of subcommands.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Version: internal.Version,
		// A command that fails prints its error, not the whole help text. The user asks
		// for help explicitly.
		SilenceUsage: true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			setupLogging(debug)
		},
	}

	persistentFlags := cmd.PersistentFlags()
	persistentFlags.BoolVar(&debug, "debug", false, "log at debug level")
	internal.EndpointFlag.Register(persistentFlags)
	internal.SkipVersionCheckFlag.Register(persistentFlags)

	cmd.AddCommand(auth.New())
	// `meshstack login` is a shortcut for `meshstack auth login`.
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
