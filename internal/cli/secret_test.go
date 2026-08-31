package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiKeySecretPrefersTheEnvironmentOverStdin(t *testing.T) {
	isolate(t)
	t.Setenv("MESHSTACK_API_SECRET", "fromtheenvironment")

	in := New()
	in.in = fileWith(t, "fromstdin\n")

	secret, err := in.ApiKeySecret(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fromtheenvironment", secret)
}

func TestApiTokenPrefersTheEnvironmentOverStdin(t *testing.T) {
	isolate(t)
	t.Setenv("MESHSTACK_API_TOKEN", "fromtheenvironment")

	in := New()
	in.in = fileWith(t, "fromstdin\n")

	token, err := in.ApiToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fromtheenvironment", token)
}

func TestApiKeySecretReadsOneLineOfStdin(t *testing.T) {
	isolate(t)

	in := New()
	in.in = fileWith(t, "fromstdin\nthis belongs to the command\n")

	secret, err := in.ApiKeySecret(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fromstdin", secret)
}

// A pipeline built with `printf %s` supplies no trailing newline, and that is the form the
// documentation shows.
func TestApiKeySecretReadsStdinWithoutATrailingNewline(t *testing.T) {
	isolate(t)

	in := New()
	in.in = fileWith(t, "fromstdin")

	secret, err := in.ApiKeySecret(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fromstdin", secret)
}

// With a terminal that may not be asked, the error has to name every way the value could
// have arrived instead of hanging on a prompt nobody will answer. /dev/null is a character
// device, which is what tty uses to recognise a terminal.
func TestApiKeySecretWithNoWayToAsk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("there is no /dev/null to stand in for a terminal")
	}
	isolate(t)

	terminal, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = terminal.Close() })

	in := New()
	in.in = terminal

	_, err = in.ApiKeySecret(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MESHSTACK_API_SECRET")
	assert.Contains(t, err.Error(), "stdin")
}

// fileWith stands in for piped stdin: a regular file is not a character device, so tty
// reports it as not a terminal, which is what makes the CLI read rather than prompt.
func fileWith(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	return file
}

// isolate points the CLI at an empty configuration directory and clears every MESHSTACK_*
// variable, so that no test reads a developer's real profile — the Taskfile loads .env into
// `task test`, so these are often set.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MESHSTACK_CONFIG_FILE", filepath.Join(dir, "config.json"))
	t.Setenv("MESHSTACK_CREDENTIALS_DIR", filepath.Join(dir, "credentials"))
	for _, key := range []string{
		"MESHSTACK_ENDPOINT", "MESHSTACK_WORKSPACE", "MESHSTACK_PROFILE",
		"MESHSTACK_API_KEY", "MESHSTACK_API_SECRET", "MESHSTACK_API_TOKEN",
	} {
		t.Setenv(key, "")
	}
	// The test process's own stdin may be a terminal, and nothing here may prompt.
	t.Setenv("MESHSTACK_NO_INPUT", "1")
}
