package buildingblock

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/definition"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/run"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "buildingblock",
		Short: "Work with meshStack building blocks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(definition.New())
	cmd.AddCommand(run.New())

	return cmd
}
