package internal

import (
	"fmt"
	"uuid"

	"github.com/spf13/cobra"
)

// UuidFlag and [UuidArg] reject a malformed uuid while cobra parses the command line. meshStack
// answers one with an empty list or a 404, which says nothing about a typo.
type UuidFlag uuid.UUID

func (f *UuidFlag) String() string {
	if *f == (UuidFlag{}) {
		return ""
	}
	return uuid.UUID(*f).String()
}

func (f *UuidFlag) Set(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return err
	}
	*f = UuidFlag(parsed)
	return nil
}

func (f *UuidFlag) Type() string {
	return "uuid"
}

func UuidArg(id *uuid.UUID) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		parsed, err := uuid.Parse(args[0])
		if err != nil {
			return fmt.Errorf("invalid argument %q: %w", args[0], err)
		}
		*id = parsed
		return nil
	}
}
