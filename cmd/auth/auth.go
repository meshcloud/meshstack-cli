package auth

import "github.com/spf13/cobra"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication with meshStack",
		// Both are needed together. Cobra returns flag.ErrHelp for a command that is
		// not runnable, before it reaches ValidateArgs, so dropping RunE would make
		// `meshstack auth bogus` print help and exit 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewLogin())

	return cmd
}
