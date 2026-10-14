package profile

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
)

func TestProfilesHoldingARequestURL(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	require.NoError(t, os.WriteFile(config.Directory(configDir).ProfilesJson(), []byte(`{"version":1,"currentProfile":"local","profiles":{
		"local":    {"endpoint":"https://localhost:1337"},
		"local-2":  {"endpoint":"https://localhost:1337"},
		"a":        {"endpoint":"https://meshstack.example.com/a"},
		"a-b":      {"endpoint":"https://meshstack.example.com/a/b/"},
		"shared-1": {"endpoint":"https://shared.example.com"},
		"shared-2": {"endpoint":"https://shared.example.com"}
	}}`), 0o600))
	profiles, err := LoadProfiles(t.Context(), LoadProfilesOptions{})
	require.NoError(t, err)
	holding := func(t *testing.T, requestURL string) (Name, bool, error) {
		t.Helper()
		parsed, err := url.Parse(requestURL)
		require.NoError(t, err)
		p, current, err := profiles.Holding(parsed)
		return p.Name, current, err
	}

	t.Run("the scheme and host match in any letter case, and of the profiles sharing them the current one is picked", func(t *testing.T) {
		name, current, err := holding(t, "https://LOCALHOST:1337/api/meshobjects")

		require.NoError(t, err)
		assert.Equal(t, Name("local"), name)
		assert.True(t, current)
	})

	t.Run("a URL on another port or scheme matches no profile, and the error says how to log in to it", func(t *testing.T) {
		for _, requestURL := range []string{"https://localhost:1338/api", "http://localhost:1337/api"} {
			_, _, err := holding(t, requestURL)

			endpoint := strings.TrimSuffix(requestURL, "/api")
			require.ErrorContains(t, err, "no stored profile has the endpoint "+endpoint)
			require.ErrorContains(t, err, "'meshstack login --endpoint "+endpoint+"'")
		}
	})

	t.Run("an endpoint path matches whole segments only", func(t *testing.T) {
		_, _, err := holding(t, "https://meshstack.example.com/ab/api")

		require.ErrorContains(t, err, "no stored profile has the endpoint https://meshstack.example.com ")
	})

	t.Run("a profile other than the current one is picked, and is not current", func(t *testing.T) {
		name, current, err := holding(t, "https://meshstack.example.com/a/api")

		require.NoError(t, err)
		assert.Equal(t, Name("a"), name)
		assert.False(t, current)
	})

	t.Run("the profile with the longest endpoint that holds the URL is picked", func(t *testing.T) {
		name, _, err := holding(t, "https://meshstack.example.com/a/b/api")

		require.NoError(t, err)
		assert.Equal(t, Name("a-b"), name)
	})

	t.Run("profiles sharing the endpoint, none of them current, need a name", func(t *testing.T) {
		_, _, err := holding(t, "https://shared.example.com/api")

		require.ErrorContains(t, err, "'shared-1', 'shared-2'")
		require.ErrorContains(t, err, NameSetting.EnvKey())
	})
}
