// Package tty answers one question: can this process interact with a person?
//
// Two rules in the design need the answer and would otherwise each grow their own
// version of it. Secret input prompts only when it can; and a profile whose
// credentials file cannot be written is fatal when interactive, because an
// `auth login` that cannot save has done pointless work, and silent when it is not,
// because a CI job cannot act on the warning.
package tty

import (
	"os"
	"sync/atomic"
)

// envKey is private for the same reason the other MESHSTACK_* names are: the message
// that names it is produced here.
const envKey = "MESHSTACK_NO_INPUT"

// disabled is set by the --no-input flag. It is process-wide because it describes the
// process, and it is only ever turned on.
var disabled atomic.Bool

// Disable turns prompting off for the rest of the process. cmd/meshstack calls it for
// --no-input.
func Disable() { disabled.Store(true) }

// IsInteractive reports whether this process may prompt. It requires stdin to be a
// terminal, because a prompt written to a pipe reads as a hang rather than a question.
func IsInteractive() bool {
	return MayInvolveAPerson() && IsTerminal(os.Stdin)
}

// MayInvolveAPerson reports whether this process may wait on somebody doing something
// outside it. It is weaker than IsInteractive on purpose: the browser login needs a URL to
// reach a person's eyes and a callback to come back, and stderr reaches eyes from a pipe just
// as well as from a terminal. Requiring a terminal there would refuse a login that would have
// worked — an agent driving the CLI, or a `meshstack login | tee` — for no gain.
//
// So the only thing that turns it off is somebody saying so: --no-input, or the environment
// variable. That is the signal that means "nobody is watching", and a command that would
// otherwise wait ten minutes for a callback should fail at once instead.
func MayInvolveAPerson() bool {
	return !disabled.Load() && os.Getenv(envKey) == ""
}

// IsTerminal reports whether f is a character device. That is the standard-library
// answer, and it is the one this CLI wants: it is wrong only for a character device
// that is not a terminal, which nothing feeds a CLI on purpose.
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
