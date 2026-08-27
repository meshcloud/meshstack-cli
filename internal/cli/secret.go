package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// readSecret applies the order the design fixes for both the API key secret and the API
// token, neither of which is ever a flag value: the environment, then one line of stdin
// when stdin is not a terminal, then a prompt without echo, and otherwise an error naming
// every way the value could have arrived.
//
// The environment outranks the prompt so that a shell which already exports the value never
// asks for it again.
func (i *Input) readSecret(ctx context.Context, fromEnv func() (string, bool), prompt string, missing func() error) (string, error) {
	if value, ok := fromEnv(); ok {
		return value, nil
	}
	if !tty.IsTerminal(i.stdin()) {
		return readLine(i.stdin())
	}
	// A terminal that may not be asked — --no-input, or MESHSTACK_NO_INPUT — is the one
	// case left, and it gets the error rather than a prompt nobody will answer.
	if !tty.IsInteractive() {
		return "", missing()
	}
	return i.promptSecret(ctx, prompt)
}

func (i *Input) promptSecret(ctx context.Context, prompt string) (string, error) {
	restore, err := i.echoOff(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(i.stderr(), "warning: echo could not be turned off (%v), so what you type will be visible.\n", err)
	}
	defer restore()

	_, _ = fmt.Fprintf(i.stderr(), "%s: ", prompt)
	value, err := readLine(i.stdin())
	// The Enter key produced no echo, so the next thing written would land on the prompt.
	_, _ = fmt.Fprintln(i.stderr())
	return value, err
}

// echoOff turns terminal echo off and returns the call that turns it back on.
//
// It runs stty rather than importing golang.org/x/term, which would be a fourth external
// dependency where .golangci.yml allows two — and every dependency this module can reach
// lands in the Terraform provider's dependency tree and from there in the public checksum
// database. stty is in POSIX and present on every unix this CLI ships for. The trade-off is
// Windows, which has no stty: there the prompt is visible and says so, because a warning a
// user can act on beats a secret echoed silently.
func (i *Input) echoOff(ctx context.Context) (restore func(), err error) {
	if runtime.GOOS == "windows" {
		return func() {}, errors.New("windows has no stty")
	}
	if err := i.stty(ctx, "-echo"); err != nil {
		return func() {}, err
	}
	return func() {
		// A context that cannot be cancelled: the caller's may already be done, and a
		// terminal left without echo outlives this process.
		if err := i.stty(context.WithoutCancel(ctx), "echo"); err != nil {
			slog.Warn("could not turn terminal echo back on; run `stty echo` to fix it", "error", err)
		}
	}, nil
}

func (i *Input) stty(ctx context.Context, arg string) error {
	cmd := exec.CommandContext(ctx, "stty", arg)
	// stty acts on the terminal it is given as stdin, which is the one being prompted on.
	cmd.Stdin = i.stdin()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if quoted := strings.TrimSpace(string(output)); quoted != "" {
			return fmt.Errorf("stty %s: %w: %s", arg, err, quoted)
		}
		return fmt.Errorf("stty %s: %w", arg, err)
	}
	return nil
}

// readLine reads exactly one line, because whatever follows it on stdin belongs to the
// command rather than to the secret. A last line without a newline is a value too: a
// `printf %s "$secret" |` pipeline is the documented way to supply one.
func readLine(f *os.File) (string, error) {
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
