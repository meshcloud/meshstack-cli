package internal

import (
	"fmt"
	"uuid"

	"github.com/spf13/cobra"
)

// UuidFlag and [UuidArg] reject a malformed uuid while cobra parses the command line. meshStack
// answers one with an empty list or a 404, which says nothing about a typo.
type UuidFlag string

func (f *UuidFlag) String() string {
	return string(*f)
}

func (f *UuidFlag) Set(value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return err
	}
	*f = UuidFlag(value)
	return nil
}

func (f *UuidFlag) Type() string {
	return "uuid"
}

func UuidArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	if _, err := uuid.Parse(args[0]); err != nil {
		return fmt.Errorf("invalid argument %q: %w", args[0], err)
	}
	return nil
}
