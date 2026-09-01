package profile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/cli"
)

// An unknown key is answered with the list of keys that exist, and before anything is
// resolved: a typo must not be reported as whatever the configuration happens to lack.
func TestSetRefusesAnUnknownKey(t *testing.T) {
	isolate(t)
	cmd := newSet(cli.New())
	cmd.SetArgs([]string{"workspaces", "my-workspace"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile setting")
	assert.Contains(t, err.Error(), "endpoint, workspace")
}

func TestSetTakesExactlyTwoArguments(t *testing.T) {
	isolate(t)
	cmd := newSet(cli.New())
	cmd.SetArgs([]string{"workspace"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true

	require.Error(t, cmd.Execute())
}

// isolate points the CLI at an empty configuration directory and clears every MESHSTACK_*
// variable, so that no test reads a developer's real profile — the Taskfile loads .env into
// `task test`, so these are often set.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MESHSTACK_CONFIG_DIR", dir)
	for _, key := range []string{
		"MESHSTACK_ENDPOINT", "MESHSTACK_WORKSPACE", "MESHSTACK_PROFILE",
		"MESHSTACK_API_KEY", "MESHSTACK_API_SECRET", "MESHSTACK_API_TOKEN",
	} {
		t.Setenv(key, "")
	}
	// The test process's own stdin may be a terminal, and nothing here may prompt.
	t.Setenv("MESHSTACK_NO_INPUT", "1")
}
