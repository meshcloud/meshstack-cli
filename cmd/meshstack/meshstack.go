package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	clog "charm.land/log/v2"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/api"
	"github.com/meshcloud/meshstack-cli/cmd/auth"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockdefinition"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockdefinitionversion"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockrun"
	"github.com/meshcloud/meshstack-cli/cmd/eventlog"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/color"
	"github.com/meshcloud/meshstack-cli/cmd/profile"
	"github.com/meshcloud/meshstack-cli/cmd/workspace"
	"github.com/meshcloud/meshstack-cli/pkg/io"
)

func main() {
	// No deadline bounds a command as a whole, since a listing takes as long as it is long. The
	// HTTP client gives up on a server that stopped answering, see internal/http.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := newRootCommand().ExecuteContext(ctx)
	stop()
	if exitErr, ok := errors.AsType[internal.ExitError](err); ok {
		os.Exit(exitErr.Code)
	} else if err != nil {
		// cobra has already written the error to stderr.
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "meshstack",
		Short: "Command line interface for meshStack",
		Long: `Command line interface for meshStack.

Start with meshstack login. It logs you in and keeps the login in a profile.

The endpoint, the profile and the workspace come from a flag, else from the environment variable
that the flag's help names, else from the profile, which holds an endpoint and a default workspace.
Without a profile named, the one whose endpoint matches is used, else the current profile, which
meshstack login sets.

AI agents and scripts: add -o json to a list or show command to read JSON. meshstack api sends a
request to any path of the meshStack API, and meshstack api-docs describes that path.

Colors follow NO_COLOR and FORCE_COLOR, and stay off in a pipe.`,
		Example: `  meshstack login
  meshstack bb list -o json
  meshstack api-docs --describe bb.list
  meshstack api /api/meshobjects/meshworkspaces`,
		// RunE has to be set for cobra to render the usage block at all: its help template
		// skips usage while the command is neither runnable nor a parent of subcommands.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Version:      internal.Version,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			setupLogging(os.Stderr, debug)
			cmd.SetContext(io.WithStderr(cmd.Context(), cmd.ErrOrStderr()))
		},
	}

	persistentFlags := cmd.PersistentFlags()
	persistentFlags.BoolVar(&debug, "debug", false, "log at debug level, and give every line its time and level")
	internal.EndpointFlag.Register(persistentFlags)
	internal.WorkspaceFlag.Register(persistentFlags)
	internal.SkipVersionCheckFlag.Register(persistentFlags)
	internal.ProfileFlag.Register(persistentFlags)

	cmd.AddCommand(api.New())
	cmd.AddCommand(api.NewDocs())
	cmd.AddCommand(auth.New())
	// Calling the constructor a second time is the only way to get a shortcut: cobra's Aliases
	// rename a command inside its own parent.
	cmd.AddCommand(auth.NewLoginShortcut())
	cmd.AddCommand(buildingblock.New())
	cmd.AddCommand(buildingblockdefinition.New())
	cmd.AddCommand(buildingblockdefinitionversion.New())
	cmd.AddCommand(buildingblockrun.New())
	cmd.AddCommand(eventlog.New())
	cmd.AddCommand(profile.New())
	cmd.AddCommand(workspace.New())

	showUsageOnMisuse(cmd)
	return cmd
}

// showUsageOnMisuse lets cobra print the usage after the error where an argument or a flag is
// wrong, and keeps it silent for an error of the command itself, which the usage does not help with.
// The usage rather than the full help keeps the error on the screen.
func showUsageOnMisuse(root *cobra.Command) {
	showUsage := func(err error) error {
		root.SilenceUsage = false
		// cobra prints the usage to the root's output, which a caller may have set to stdout.
		// Nothing but the usage is written there once the command failed.
		root.SetOut(root.ErrOrStderr())
		return err
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return showUsage(err)
	})
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if args := cmd.Args; args != nil {
			cmd.Args = func(cmd *cobra.Command, positional []string) error {
				if cmd.HasSubCommands() && len(positional) > 0 {
					return showUsage(unknownSubcommand(cmd, positional[0]))
				}
				if err := args(cmd, positional); err != nil {
					return showUsage(err)
				}
				return nil
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// unknownSubcommand is cobra's own error for an unknown subcommand, with its suggestions. Cobra
// only suggests for a root that sets no Args, while every parent here sets Args and RunE, so that
// its help shows the usage.
func unknownSubcommand(parent *cobra.Command, name string) error {
	if parent.SuggestionsMinimumDistance <= 0 {
		parent.SuggestionsMinimumDistance = 2
	}
	var suggestions strings.Builder
	if suggested := parent.SuggestionsFor(name); len(suggested) > 0 {
		suggestions.WriteString("\n\nDid you mean this?\n")
		for _, s := range suggested {
			_, _ = fmt.Fprintf(&suggestions, "\t%s\n", s)
		}
	}
	return fmt.Errorf("unknown command %q for %q%s", name, parent.CommandPath(), suggestions.String())
}

// setupLogging writes an INFO record as a plain line, since most of them tell the user what a
// command did. With --debug the log becomes a trace, and every line gets its time and level back.
func setupLogging(stderr *os.File, debug bool) {
	options := clog.Options{ReportTimestamp: debug, Level: clog.InfoLevel}
	if debug {
		options.Level = clog.DebugLevel
	}
	logger := clog.NewWithOptions(stderr, options)
	if !debug {
		styles := clog.DefaultStyles()
		// The text formatter writes no level for a level without a style.
		delete(styles.Levels, clog.InfoLevel)
		logger.SetStyles(styles)
	}
	logger.SetColorProfile(color.Log(stderr, os.Environ()))
	slog.SetDefault(slog.New(logger))
}
