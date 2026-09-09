package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/cli"
)

// testSecret has the shape credential.CheckSecret expects: 32 alphanumerics, no whitespace,
// not a UUID.
const testSecret = "abcdef0123456789abcdef0123456789"

// --api-key takes an optional value, and pflag resolves such a flag before it looks at the
// next argument. So the equals form is the only one that can carry an id, and the two ways
// of getting that wrong each have to say so.
func TestApiKeyFlagForms(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantId  string
		wantErr string
	}{
		{
			name: "bare reuses the id already in the profile",
			args: []string{"--api-key"},
		},
		{
			name:   "the equals form carries a new id",
			args:   []string{"--api-key=0000-0001"},
			wantId: "0000-0001",
		},
		{
			name:    "a separate id is not an argument",
			args:    []string{"--api-key", "0000-0001"},
			wantErr: "equals sign",
		},
		{
			name:    "an empty id is refused",
			args:    []string{"--api-key="},
			wantErr: "the API key id is empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isolateAt(t)
			in := cli.New()
			err := run(NewLogin(in), test.args...)

			require.Error(t, err, "no profile is configured, so every form fails eventually")
			if test.wantErr != "" {
				assert.Contains(t, err.Error(), test.wantErr)
				assert.Empty(t, in.ApiKey, "the flag was refused, so nothing reached the source")
				return
			}
			// The form was accepted; what stopped it is the missing endpoint further on.
			assert.Contains(t, err.Error(), "endpoint")
			assert.Equal(t, test.wantId, in.ApiKey)
		})
	}
}

// A secret is never a flag value, so --api-secret-stdin is how one reaches the resolution
// without landing in shell history, in ps output or in a CI log.
func TestApiSecretStdinReachesTheProfile(t *testing.T) {
	dir := isolateAt(t)
	stack := devLocalStack(t, false)
	stdinWith(t, testSecret+"\n")

	require.NoError(t, run(loginWithRootFlags(cli.New()),
		"--endpoint", stack, "--api-key=key-42", "--api-secret-stdin"))

	credentials, err := os.ReadFile(filepath.Join(dir, "credentials", "default.json"))
	require.NoError(t, err)
	assert.Contains(t, string(credentials), "key-42")
	assert.Contains(t, string(credentials), testSecret)
	assert.Contains(t, string(credentials), `"current": "apiKey"`)
}

func TestApiTokenStdinReachesTheProfile(t *testing.T) {
	dir := isolateAt(t)
	stdinWith(t, devLocalToken()+"\n")

	require.NoError(t, run(loginWithRootFlags(cli.New()),
		"--endpoint", "https://api.example.com", "--api-token", "--api-token-stdin"))

	credentials, err := os.ReadFile(filepath.Join(dir, "credentials", "default.json"))
	require.NoError(t, err)
	assert.Contains(t, string(credentials), `"current": "manual"`)
}

func TestApiKeyAndApiTokenAreMutuallyExclusive(t *testing.T) {
	isolateAt(t)

	err := run(NewLogin(cli.New()), "--api-key", "--api-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api-key")
	assert.Contains(t, err.Error(), "api-token")
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestLoginTakesNoArguments(t *testing.T) {
	isolateAt(t)

	err := run(NewLogin(cli.New()), "workspace-name")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not take")
}

func run(cmd *cobra.Command, args ...string) error {
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewReader(nil))
	// A usage dump would drown the assertions, and the real root silences it too.
	cmd.SilenceUsage = true
	return cmd.Execute()
}

// stdinWith replaces the process's stdin, because the two --*-stdin flags read a file rather
// than cobra's input stream: a terminal check and a prompt both need the real descriptor.
// cli.New captures it, so this has to run first.
func stdinWith(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	previous := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = previous
		_ = file.Close()
	})
}
