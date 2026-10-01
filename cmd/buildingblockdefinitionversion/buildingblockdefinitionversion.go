package buildingblockdefinitionversion

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "buildingblockdefinitionversion",
		Aliases: internal.KindAliases("buildingblockdefinitionversion"),
		Short:   "Work with meshStack building block definition versions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())

	return cmd
}
