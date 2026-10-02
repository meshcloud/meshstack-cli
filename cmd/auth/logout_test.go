package auth

import (
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/logs"
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

func TestLogoutOfAProfileThatDoesNotExistOnlyWarns(t *testing.T) {
	configDir := withEmptyConfigDir(t)

	captured := logs.Capture(t)

	_, err := execute(t, "auth", "logout", "-p", "missing")

	require.NoError(t, err)
	assert.Contains(t, captured.String(), "level=WARN")
	assert.Contains(t, captured.String(), "no stored profile is named 'missing'")
	assert.NoFileExists(t, configDir.ProfilesJson(), "the logout created no profile")
}

func TestLogoutRemovesTheCredentialsOfTheProfile(t *testing.T) {
	withEmptyConfigDir(t)
	profiles, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{})
	require.NoError(t, err)
	dev := profiles.Add(profile.Profile{Name: "dev", Endpoint: xurl.MustParsef("https://localhost:1")})
	require.NoError(t, profiles.Store(t.Context()))
	creds, err := dev.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(&credential.ApiKey{Endpoint: dev.Endpoint, ClientId: uuid.MustParse("11111111-45bf-42ba-a965-2097b9d0d181"), ClientSecret: "secret"})
	require.NoError(t, creds.Store(t.Context()))

	captured := logs.Capture(t)

	_, err = execute(t, "auth", "logout", "-p", "dev")

	require.NoError(t, err)
	assert.Contains(t, captured.String(), `level=INFO msg="Logged out of profile 'dev'"`)
	assert.NoFileExists(t, creds.FilePath)
}
