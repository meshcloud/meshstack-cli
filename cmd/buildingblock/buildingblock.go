package buildingblock

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblocktfstate"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const command internal.KindCommand = "buildingblock"
	const short = "Work with meshStack building blocks"
	cmd := &cobra.Command{
		Use:     string(command),
		Aliases: command.Aliases(),
		Short:   short,
		Long:    command.Long(short),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newTriggerRun())
	cmd.AddCommand(buildingblocktfstate.New())

	return cmd
}
