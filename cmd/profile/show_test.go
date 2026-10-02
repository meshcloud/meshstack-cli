package profile

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"maps"
	gohttp "net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
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
		assert.Contains(t, output, "|  | No token cached yet |\n")
		assert.Contains(t, output, "This is the current profile.")
	})

	t.Run("shows the current profile with a warning where it is for another endpoint, and that endpoint has several profiles", func(t *testing.T) {
		require.NoError(t, profiles.Put(t.Context(), nil, profile.Profile{Name: "staging", Endpoint: endpointB}))
		captured := logs.Capture(t)

		output, err := execute(t, "", "show", "--endpoint", "https://b.example.io")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | dev |\n")
		assert.Contains(t, captured.String(), "Profile 'dev' is for endpoint 'https://a.example.io', so a command for endpoint 'https://b.example.io' (from flag --endpoint) fails with it")
	})

	t.Run("shows the profile a first command would create, without storing it, where no profile is current", func(t *testing.T) {
		profiles.CurrentProfile = ""
		require.NoError(t, profiles.Store(t.Context()))

		output, err := execute(t, "", "show", "--endpoint", "https://b.example.io")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | default |\n| --- | --- |\n| Endpoint | https://b.example.io |\n")
		assert.Contains(t, output, "This profile is not stored yet, a first login stores it.")
		requireStoredNames(t, "dev", "prod", "staging")
	})

	t.Run("shows the profile a first command would create without any profile or endpoint", func(t *testing.T) {
		emptyConfigDir(t)

		output, err := execute(t, "", "show")

		require.NoError(t, err)
		assert.Contains(t, output, "| Profile | default |\n| --- | --- |\n| Endpoint | none |\n| Default workspace | none |\n")
		assert.Contains(t, output, "This profile is not stored yet, a first login stores it.")
		requireStoredNames(t)
	})
}

func requireStoredNames(t *testing.T, want ...profile.Name) {
	t.Helper()
	stored, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
	require.NoError(t, err)
	assert.ElementsMatch(t, want, slices.Collect(maps.Keys(stored.Profiles)))
}

func TestShowSaysWhereTheStatusCannotBeReadInTime(t *testing.T) {
	unreachable := httptest.NewServer(gohttp.HandlerFunc(func(_ gohttp.ResponseWriter, r *gohttp.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(unreachable.CloseClientConnections)
	endpoint := xurl.MustParsef("%s", unreachable.URL)
	profiles := storedProfiles(t, profile.Profile{Name: "ci", Endpoint: endpoint, Credential: credential.ApiKeyName})
	credentials, err := profiles.Profiles["ci"].Credentials(t.Context())
	require.NoError(t, err)
	credentials.Set(&credential.ApiKey{Endpoint: endpoint, ClientId: uuid.MustParse("11111111-45bf-42ba-a965-2097b9d0d181"), ClientSecret: "test-secret"})
	require.NoError(t, credentials.Store(t.Context()))
	logs.Capture(t)
	// A deadline of the caller stands in for statusReadTime, which a test would wait for in full.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	s := showOf(ctx, profiles, profiles.Profiles["ci"])

	assert.Nil(t, s.Status)
	shownMarkdown, err := markdown.Execute(showTemplate, s)
	require.NoError(t, err)
	assert.Contains(t, shownMarkdown, "| Endpoint | "+unreachable.URL+" |\n")
	assert.Contains(t, shownMarkdown, "| Credential | API key, whose status could not be read: context deadline exceeded |\n")
}
