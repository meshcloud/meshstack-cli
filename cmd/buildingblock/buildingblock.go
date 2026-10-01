package buildingblock

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const short = "Work with meshStack building blocks"
	cmd := &cobra.Command{
		Use:     "buildingblock",
		Aliases: internal.KindAliases("buildingblock"),
		Short:   short,
		Long:    internal.KindLong(short, "buildingblock"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newTriggerRun())

	return cmd
}
