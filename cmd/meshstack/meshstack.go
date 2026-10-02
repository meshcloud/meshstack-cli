package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"

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

	return cmd
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
