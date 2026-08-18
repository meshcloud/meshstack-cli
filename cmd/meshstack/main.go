// Command meshstack is the command line interface for meshStack.
//
// This package holds the root command. Every other package under cmd/ follows one
// rule: the package name is the subcommand and the file name is the leaf command,
// so cmd/buildingblock/list.go holds `meshstack buildingblock list`. Each of those
// packages exports a New function returning its *cobra.Command, and this package
// wires them in with AddCommand. Registration is explicit rather than done from
// init(), so the whole command tree can be read in one place and a command cannot
// appear in the binary just because its package was imported for another reason.
//
// The directory is named meshstack, not meshstack-cli, because `go build` and
// `go install` name the binary after it.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

// Version identifies this build. A release overrides it with
// -ldflags "-X main.Version=<tag>", and it also identifies the CLI to the meshStack
// API through the client's user agent.
var Version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		// cobra has already written the error to stderr.
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "meshstack",
		Short: "Command line interface for meshStack",
		// Running `meshstack` on its own prints the help text. RunE also has to be set
		// for cobra to render the usage block at all: its help template skips usage
		// while the command is neither runnable nor a parent of subcommands.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Version: Version,
		// A command that fails prints its error, not the whole help text. The user asks
		// for help explicitly.
		SilenceUsage: true,
	}
}
