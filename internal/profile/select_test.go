package profile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

func TestProfilesSharingAnEndpoint(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	require.NoError(t, os.WriteFile(config.Directory(configDir).ProfilesJson(), []byte(`{"version":1,"currentProfile":"local","profiles":{
		"local":    {"endpoint":"https://localhost:1337"},
		"local-2":  {"endpoint":"https://localhost:1337"},
		"shared-1": {"endpoint":"https://shared.example.com"},
		"shared-2": {"endpoint":"https://shared.example.com"}
	}}`), 0o600))

	t.Run("the current one of them is taken", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://localhost:1337")

		resolved, _, err := ResolveProfile(t.Context(), nil)

		require.NoError(t, err)
		assert.Equal(t, Name("local"), resolved.Name)
	})

	t.Run("none of them is taken where none is current", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://shared.example.com")

		resolved, profiles, err := ResolveProfile(t.Context(), nil)

		require.NoError(t, err)
		assert.Empty(t, resolved.Name)
		assert.Equal(t, []Name{"shared-1", "shared-2"}, names(pointersTo(profiles.ForEndpoint(resolved.Endpoint))))
	})
}

func pointersTo(profiles []Profile) (pointers []*Profile) {
	for i := range profiles {
		pointers = append(pointers, &profiles[i])
	}
	return pointers
}
