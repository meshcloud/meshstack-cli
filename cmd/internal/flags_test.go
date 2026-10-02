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

func TestListWorkspaceTakesTheProfileDefaultUnlessASettingNamesOne(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "profiles.json"),
		[]byte(`{"version":1,"currentProfile":"default","profiles":{"default":{"endpoint":"https://meshstack.example.com","default_workspace":"from-profile"}}}`), 0o600))
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "")
	t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "")

	workspace, err := internal.ListWorkspace(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-profile", *workspace)

	t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "from-env")
	workspace, err = internal.ListWorkspace(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-env", *workspace)
}
