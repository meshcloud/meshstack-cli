package internal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

func TestTheEnvironmentDecidesWhileTheBoolFlagIsUnset(t *testing.T) {
	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")

	skip, err := internal.SettingSources().ResolveSetting(t.Context(), meshstack.SkipVersionCheckSetting)

	require.NoError(t, err)
	assert.True(t, skip)
}

// A listing asked for no workspace shows what the credential can see, so a profile's default
// workspace narrows it no more than an absent flag does.
func TestListWorkspaceIgnoresTheProfileDefault(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "profiles.json"),
		[]byte(`{"version":1,"currentProfile":"default","profiles":{"default":{"default_workspace":"from-profile"}}}`), 0o600))
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "")

	workspace, err := internal.ListWorkspace(t.Context())

	require.NoError(t, err)
	assert.Nil(t, workspace)
}
