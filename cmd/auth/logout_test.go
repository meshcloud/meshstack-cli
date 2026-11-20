package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func withEmptyConfigDir(t *testing.T) config.Directory {
	t.Helper()
	for _, envKey := range []string{profile.NameSetting.EnvKey(), meshstack.EndpointSetting.EnvKey()} {
		t.Setenv(envKey, "")
	}
	configDir := config.Directory(t.TempDir())
	t.Setenv(config.DirectorySetting.EnvKey(), string(configDir))
	return configDir
}

func TestLogoutOfAProfileThatDoesNotExistFails(t *testing.T) {
	configDir := withEmptyConfigDir(t)

	_, err := execute(t, "auth", "logout", "-p", "missing")

	require.ErrorContains(t, err, "there is no profile 'missing'")
	assert.NoFileExists(t, configDir.ProfilesJson(), "the logout created no profile")
}
