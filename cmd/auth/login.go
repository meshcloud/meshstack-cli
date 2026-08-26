package auth

import (
	"errors"

	"github.com/spf13/cobra"
)

// NewLogin returns a fresh command on every call, because cmd/meshstack registers
// login under two parents. Flag targets have to stay local to this function: a
// package-level var would be shared by both instances.
func NewLogin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// TODO: resolve credentials through pkg/login and cache the token on disk.
			return errors.New("login is not implemented yet")
		},
	}

	return cmd
}
