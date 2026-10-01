package workspace

import (
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	const short = "Work with meshStack workspaces"
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: internal.KindAliases("workspace"),
		Short:   short,
		Long:    internal.KindLong(short, "workspace"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())

	return cmd
}
