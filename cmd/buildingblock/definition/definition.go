package definition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/definition/version"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "definition",
		Short: "Work with building block definitions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(version.New())

	return cmd
}
