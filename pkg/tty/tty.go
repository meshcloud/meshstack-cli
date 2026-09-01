// Package tty declares MESHSTACK_NO_INPUT and answers the one question a declaration
// cannot: whether a file is a terminal.
//
// It holds no state. Whether this process may wait on a person is the NoInput setting,
// resolved from ranked sources like everything else, and each owner of the answer keeps
// its own copy — pkg/auth on the Session, the meshStack CLI on its Input.
package tty

import "os"

// envKey is private for the same reason the other MESHSTACK_* names are: the message
// that names it is produced here.
const envKey = "MESHSTACK_NO_INPUT"

// IsTerminal is the standard-library answer, a character-device check, which is wrong only
// for a character device that is not a terminal — nothing feeds a CLI one on purpose.
//
// It is a separate question from NoInput because a prompt needs it and a browser login does
// not: stderr reaches a person from a pipe just as well as from a terminal.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// NoInputHint names the two ways prompting gets turned off, for an error that has to
// explain why it did not ask.
func NoInputHint() string {
	return "--no-input or " + envKey
}
