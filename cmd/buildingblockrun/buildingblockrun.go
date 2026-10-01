package buildingblockrun

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "buildingblockrun",
		Aliases: internal.KindAliases("buildingblockrun"),
		Short:   "Work with meshStack building block runs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newLogs())

	return cmd
}
