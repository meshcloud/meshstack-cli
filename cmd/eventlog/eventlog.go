package eventlog

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const short = "Work with meshStack event logs"
	cmd := &cobra.Command{
		Use:     "eventlog",
		Aliases: internal.KindAliases("eventlog"),
		Short:   short,
		Long:    internal.KindLong(short, "eventlog"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())

	return cmd
}
