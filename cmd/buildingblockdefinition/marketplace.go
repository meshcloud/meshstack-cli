package buildingblockdefinition

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newMarketplace() *cobra.Command {
	var flags internal.ListFlags

	cmd := &cobra.Command{
		Use:     "marketplace",
		Short:   "List the building block definitions that the workspace can use, newest first",
		Long:    "List the building block definitions of the workspace you work in and those published to the whole platform, newest first. The command works like 'meshstack buildingblockdefinition list' with --" + includeAllPublishedFlagName + ".",
		Example: `  meshstack bbd marketplace`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return list(cmd, &flags, true)
		},
	}

	flags.Register(cmd.Flags())

	return cmd
}
