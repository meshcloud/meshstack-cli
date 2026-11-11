package profile

import (
	"encoding/json/v2"
	"log/slog"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func TestList(t *testing.T) {
	emptyConfigDir(t)
	t.Run("without profiles says how to add one", func(t *testing.T) {
		output, err := execute(t, "", "list")

		require.NoError(t, err)
		assert.Contains(t, output, "No profiles yet.")
	})

	twoProfiles(t)
	t.Run("shows every profile and marks the current one", func(t *testing.T) {
		output, err := execute(t, "", "list")

		require.NoError(t, err)
		assert.Equal(t, "| | Profile | Endpoint | Default workspace | Credential |\n"+
			"| --- | --- | --- | --- | --- |\n"+
			"| current | dev | https://a.example.io |  |  |\n"+
			"|  | prod | https://b.example.io | ops |  |\n", output)
	})

	t.Run("writes JSON", func(t *testing.T) {
		output, err := execute(t, "", "list", "-o", "json")

		require.NoError(t, err)
		var listed []map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &listed))
		assert.Equal(t, []map[string]any{
			{"name": "dev", "endpoint": "https://a.example.io", "current": true},
			{"name": "prod", "endpoint": "https://b.example.io", "default_workspace": "ops", "current": false},
		}, listed)
	})
}

func TestShow(t *testing.T) {
	t.Setenv("MESHSTACK_ENDPOINT", "")
	profiles := twoProfiles(t)

	t.Run("takes the profile the environment names", func(t *testing.T) {
		t.Setenv("MESHSTACK_PROFILE", "prod")

		output, err := execute(t, "", "show")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | prod |\n")
	})

	t.Run("writes JSON for the only profile of the endpoint", func(t *testing.T) {
		captured := logs.Capture(t)

		output, err := execute(t, "", "show", "--endpoint", "https://b.example.io", "-o", "json")

		require.NoError(t, err)
		var shown map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &shown))
		assert.Equal(t, "prod", shown["name"])
		assert.NotContains(t, shown, "status", "a profile without a credential has no status")
		assert.Empty(t, captured.Lines(slog.LevelInfo), "a profile never logged in is no reason to warn")
	})

	t.Run("takes the current profile with its stored credential", func(t *testing.T) {
		dev := profiles.Profiles["dev"]
		storeCredentials(t, dev)
		dev.Credential = credential.ManualName
		require.NoError(t, profiles.Store(t.Context()))

		output, err := execute(t, "", "show")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | dev |\n")
		assert.Contains(t, output, "| Credential | API token, from file ")
		assert.Contains(t, output, "|  | Token expires 2100-01-01")
		assert.Contains(t, output, "This is the current profile.")
	})

	t.Run("shows the endpoint alone where several profiles have it and none of them is current", func(t *testing.T) {
		require.NoError(t, profiles.Put(t.Context(), nil, profile.Profile{Name: "staging", Endpoint: endpointB}))

		output, err := execute(t, "", "show", "--endpoint", "https://b.example.io")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | none |\n| --- | --- |\n| Endpoint | https://b.example.io |\n")
		assert.Contains(t, output, "| Credential | none, run `meshstack login --endpoint https://b.example.io` |\n")
		assert.Contains(t, output, "No stored profile is used. A login saves one.")
		requireStoredNames(t, "dev", "prod", "staging")
	})

	t.Run("fails without any profile or endpoint", func(t *testing.T) {
		emptyConfigDir(t)

		_, err := execute(t, "", "show")

		require.ErrorContains(t, err, "or run 'meshstack login'")
	})
}

func requireStoredNames(t *testing.T, want ...profile.Name) {
	t.Helper()
	stored, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
	require.NoError(t, err)
	assert.ElementsMatch(t, want, slices.Collect(maps.Keys(stored.Profiles)))
}
