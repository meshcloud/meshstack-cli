package profile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// secretCommandTimeout bounds clientSecretCommand, so that a secret store which has
// become unreachable fails rather than hangs. It is generous, because reaching a vault
// through a tunnel is slow but not minutes slow.
const secretCommandTimeout = 30 * time.Second

// maxQuotedStderr caps how much of a failed command's stderr reaches the error message.
// The whole of it already went to the process's stderr, where the user can read it.
const maxQuotedStderr = 2000

// Secret returns the API key secret: the stored one, or the output of
// clientSecretCommand. It runs under a fixed timeout so that a hanging secret store
// fails rather than hangs, and pkg/auth calls it only at the moment a secret is
// actually needed, so a command served from a cached token never runs it.
func (m ApiKeyMethod) Secret(ctx context.Context) (string, error) {
	if m.ClientSecret != "" {
		return m.ClientSecret, nil
	}
	if len(m.ClientSecretCommand) == 0 {
		return "", diags.Errorf("The API key has no secret",
			"This profile stores neither a secret nor a clientSecretCommand for API key %s. Log in again to store one.", m.ClientId)
	}

	// The argument list may be logged; its output never is.
	slog.Debug("running clientSecretCommand", "command", m.ClientSecretCommand)

	ctx, cancel := context.WithTimeout(ctx, secretCommandTimeout)
	defer cancel()

	// An argument list, never a shell string: no quoting rules and no shell injection.
	cmd := exec.CommandContext(ctx, m.ClientSecretCommand[0], m.ClientSecretCommand[1:]...)
	// The caller's environment reaches the command, because that is where a secret store
	// finds its own configuration: VAULT_ADDR, OP_SERVICE_ACCOUNT_TOKEN.
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	// stderr passes through as well as being captured, so vault's own "token expired"
	// message reaches the user while the error can still quote it.
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		detail := fmt.Sprintf("clientSecretCommand `%s` failed: %v.", commandLine(m.ClientSecretCommand), err)
		if quoted := strings.TrimSpace(stderr.String()); quoted != "" {
			detail += " It wrote: " + truncate(quoted, maxQuotedStderr)
		}
		if ctx.Err() != nil {
			detail += fmt.Sprintf(" It was given %s to produce the secret on stdout.", secretCommandTimeout)
		}
		return "", diags.Wrap(err, "Cannot fetch the API key secret", "%s", detail)
	}

	secret := strings.TrimSuffix(strings.TrimSuffix(stdout.String(), "\n"), "\r")
	if err := CheckSecret(secret); err != nil {
		return "", diags.Wrap(err, "The API key secret command produced no usable secret",
			"clientSecretCommand `%s` must print the secret and nothing else on stdout. %v", commandLine(m.ClientSecretCommand), err)
	}
	return secret, nil
}

// CheckSecret rejects a value that cannot be an API key secret, wherever it came from —
// the environment, stdin, a prompt, or clientSecretCommand.
//
// It checks shape and not size: a generated secret is 32 alphanumerics, so a length
// rule would be arbitrary and would miss both real mistakes. A truncated secret already
// gets a clear 401 from /api/login, which is the real judge.
func CheckSecret(secret string) error {
	if strings.TrimSpace(secret) == "" {
		return diags.Errorf("The API key secret is empty",
			"Nothing supplied a secret. A blank line from a secret helper is the usual cause.")
	}
	if strings.ContainsFunc(secret, unicode.IsSpace) {
		return diags.Errorf("The API key secret contains whitespace",
			"An API key secret is 32 alphanumeric characters, so a value with a newline or a space in it is usually a secret helper printing a diagnostic rather than a secret.")
	}
	if isUUID(secret) {
		return diags.Errorf("That is the API key id, not its secret",
			"The value is a UUID, and an API key secret is 32 alphanumeric characters and never a UUID. The id belongs in --api-key; the secret is the other half meshPanel showed next to it.")
	}
	return nil
}

// isUUID matches the 8-4-4-4-12 hex shape by hand, because recognising the id pasted
// into the secret prompt is not worth a dependency.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// commandLine renders an argument list for a message. It is not shell syntax and is
// never re-parsed; only the first word is a program name.
func commandLine(args []string) string {
	return strings.Join(args, " ")
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
