package buildingblockrun

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const short = "Work with meshStack building block runs"
	cmd := &cobra.Command{
		Use:     "buildingblockrun",
		Aliases: internal.KindAliases("buildingblockrun"),
		Short:   short,
		Long:    internal.KindLong(short, "buildingblockrun"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newLogs())

	return cmd
}
