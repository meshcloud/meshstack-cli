package auth

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
)

// --api-key takes an optional value, and pflag resolves such a flag before it looks at the
// next argument. So the equals form is the only one that can carry an id, and the two ways
// of getting that wrong each have to say so.
func TestApiKeyFlagForms(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod credential.Method
		wantId     string
		wantErr    string
	}{
		{
			name:       "bare reuses the id already in the profile",
			args:       []string{"--api-key"},
			wantMethod: credential.MethodApiKey,
		},
		{
			name:       "the equals form carries a new id",
			args:       []string{"--api-key=0000-0001"},
			wantMethod: credential.MethodApiKey,
			wantId:     "0000-0001",
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
			isolate(t)
			in := cli.New()
			cmd := NewLogin(in)
			err := run(cmd, test.args...)

			require.Error(t, err, "no profile is configured, so every form fails eventually")
			if test.wantErr != "" {
				assert.Contains(t, err.Error(), test.wantErr)
				assert.Empty(t, in.Method, "the flag was refused, so no method was demanded")
				return
			}
			// The form was accepted; what stopped it is the missing endpoint further on.
			assert.Contains(t, err.Error(), "endpoint")
			assert.Equal(t, test.wantMethod, in.Method)
			assert.Equal(t, test.wantId, in.ApiKey)
		})
	}
}

func TestApiKeyAndApiTokenAreMutuallyExclusive(t *testing.T) {
	isolate(t)
	in := cli.New()

	err := run(NewLogin(in), "--api-key", "--api-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api-key")
	assert.Contains(t, err.Error(), "api-token")
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestLoginTakesNoArguments(t *testing.T) {
	isolate(t)

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
