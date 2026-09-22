package buildingblock

import (
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "buildingblock",
		Aliases: []string{"bb"},
		Short:   "Work with meshStack building blocks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newTriggerRun())

	return cmd
}
