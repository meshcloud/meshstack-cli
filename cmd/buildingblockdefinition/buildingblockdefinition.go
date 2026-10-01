package buildingblockdefinition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const short = "Work with meshStack building block definitions"
	cmd := &cobra.Command{
		Use:     "buildingblockdefinition",
		Aliases: internal.KindAliases("buildingblockdefinition"),
		Short:   short,
		Long:    internal.KindLong(short, "buildingblockdefinition"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())

	return cmd
}
