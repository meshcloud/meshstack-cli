package api

import (
	"log/slog"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func TestProfileForARequestURL(t *testing.T) {
	for _, envKey := range []string{profile.NameSetting.EnvKey(), meshstack.EndpointSetting.EnvKey()} {
		t.Setenv(envKey, "")
	}
	configDir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	require.NoError(t, os.WriteFile(config.Directory(configDir).ProfilesJson(), []byte(`{"version":1,"currentProfile":"local","profiles":{
		"local":    {"endpoint":"https://localhost:1337"},
		"a":        {"endpoint":"https://meshstack.example.com/a"},
		"a-b":      {"endpoint":"https://meshstack.example.com/a/b/"},
		"shared-1": {"endpoint":"https://shared.example.com"},
		"shared-2": {"endpoint":"https://shared.example.com"}
	}}`), 0o600))
	resolve := func(t *testing.T, requestURL string) (endpoint string, name profile.Name, warnings []string, err error) {
		t.Helper()
		parsed, err := url.Parse(requestURL)
		require.NoError(t, err)
		captured := logs.Capture(t)
		cutAt, sources, err := profileFor(t.Context(), parsed)
		for _, record := range captured.Records(slog.LevelWarn) {
			warnings = append(warnings, record.Message)
		}
		if err != nil {
			return "", "", warnings, err
		}
		name, err = sources.ResolveSetting(t.Context(), profile.NameSetting)
		require.NoError(t, err)
		return cutAt.String(), name, warnings, nil
	}

	t.Run("a profile other than the current one is picked with a warning that names both", func(t *testing.T) {
		endpoint, name, warnings, err := resolve(t, "https://meshstack.example.com/a/api")

		require.NoError(t, err)
		assert.Equal(t, "https://meshstack.example.com/a", endpoint)
		assert.Equal(t, profile.Name("a"), name)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "'a'")
		assert.Contains(t, warnings[0], "'local'")
	})

	t.Run("profiles sharing the endpoint, none of them current, fail until the environment names one", func(t *testing.T) {
		_, _, _, err := resolve(t, "https://shared.example.com/api")

		require.ErrorContains(t, err, "'shared-1', 'shared-2'")

		t.Setenv(profile.NameSetting.EnvKey(), "shared-2")
		endpoint, name, warnings, err := resolve(t, "https://shared.example.com/api")

		require.NoError(t, err)
		assert.Equal(t, "https://shared.example.com", endpoint)
		assert.Equal(t, profile.Name("shared-2"), name)
		assert.Empty(t, warnings)
	})

	t.Run("an explicit profile is picked over a longer endpoint, and fails where it does not hold the URL", func(t *testing.T) {
		t.Setenv(profile.NameSetting.EnvKey(), "a")
		endpoint, name, _, err := resolve(t, "https://meshstack.example.com/a/b/api")

		require.NoError(t, err)
		assert.Equal(t, "https://meshstack.example.com/a", endpoint)
		assert.Equal(t, profile.Name("a"), name)

		_, _, _, err = resolve(t, "https://localhost:1337/api")

		require.ErrorContains(t, err, "https://localhost:1337/api is for endpoint https://localhost:1337, "+
			"not for endpoint https://meshstack.example.com/a of profile 'a' from env "+profile.NameSetting.EnvKey())
	})

	t.Run("an explicit endpoint fails where it does not hold the URL", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://localhost:1337")

		_, _, _, err := resolve(t, "https://meshstack.example.com/a/api")

		require.ErrorContains(t, err, "https://meshstack.example.com/a/api is for endpoint https://meshstack.example.com/a, "+
			"not for endpoint https://localhost:1337 from env "+meshstack.EndpointSetting.EnvKey())
	})

	t.Run("with no profile stored, an explicit endpoint that holds the URL leaves the name of the first profile to the session", func(t *testing.T) {
		require.NoError(t, os.Remove(config.Directory(configDir).ProfilesJson()))
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://localhost:1337")

		endpoint, name, _, err := resolve(t, "https://localhost:1337/api")

		require.NoError(t, err)
		assert.Equal(t, "https://localhost:1337", endpoint)
		assert.Equal(t, profile.Name("default"), name)
	})
}
