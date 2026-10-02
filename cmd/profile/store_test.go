package profile

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

var (
	endpointA = xurl.MustParsef("https://a.example.io")
	endpointB = xurl.MustParsef("https://b.example.io")
	endpointC = xurl.MustParsef("https://c.example.io")
)

// emptyConfigDir keeps every test away from the configuration of whoever runs it.
func emptyConfigDir(t *testing.T) profile.Profiles {
	t.Helper()
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
	profiles, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
	require.NoError(t, err)
	return profiles
}

func storedProfiles(t *testing.T, stored ...profile.Profile) profile.Profiles {
	t.Helper()
	profiles := emptyConfigDir(t)
	for _, p := range stored {
		require.NoError(t, profiles.Put(t.Context(), nil, p))
	}
	return profiles
}

func storeCredentials(t *testing.T, p *profile.Profile) {
	t.Helper()
	credentials, err := p.Credentials(t.Context())
	require.NoError(t, err)
	credentials.Manual = &credential.Manual{Endpoint: p.Endpoint}
	require.NoError(t, credentials.Store(t.Context()))
}

func loadedCredentials(t *testing.T, profiles profile.Profiles, name profile.Name) *credential.Manual {
	t.Helper()
	credentials, err := profiles.Profiles[name].Credentials(t.Context())
	require.NoError(t, err)
	return credentials.Manual
}

func TestTheStoredProfiles(t *testing.T) {
	profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA, DefaultWorkspace: "ws"}, profile.Profile{Name: "prod", Endpoint: endpointB})
	reloadedCurrent := func(t *testing.T) profile.Name {
		t.Helper()
		reloaded, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{SettingSources: internal.SettingSources()})
		require.NoError(t, err)
		return reloaded.CurrentProfile
	}
	removeWarning := func(t *testing.T, name profile.Name) string {
		t.Helper()
		captured := logs.Capture(t)
		require.NoError(t, remove(t.Context(), &profiles, name))
		return strings.Join(captured.Lines(slog.LevelInfo), "\n")
	}

	t.Run("the first profile added becomes the current one", func(t *testing.T) {
		assert.Equal(t, profile.Name("dev"), reloadedCurrent(t))
		assert.Equal(t, []profile.Name{"dev", "prod"}, names(profiles))
		assert.NotEmpty(t, profiles.Profiles["prod"].ConfigDir, "an added profile knows its configuration directory")
	})

	t.Run("a name that is taken is refused", func(t *testing.T) {
		require.EqualError(t, profiles.Put(t.Context(), nil, profile.Profile{Name: "dev", Endpoint: endpointB}),
			"a profile named 'dev' exists already")
		require.EqualError(t, profiles.Put(t.Context(), profiles.Profiles["prod"], profile.Profile{Name: "dev", Endpoint: endpointB}),
			"a profile named 'dev' exists already")
	})

	t.Run("a rename takes the credentials and the current profile along", func(t *testing.T) {
		storeCredentials(t, profiles.Profiles["dev"])
		renamed := *profiles.Profiles["dev"]
		renamed.Name = "development"

		require.NoError(t, profiles.Put(t.Context(), profiles.Profiles["dev"], renamed))

		assert.Equal(t, []profile.Name{"development", "prod"}, names(profiles))
		assert.Equal(t, profile.Name("development"), profiles.CurrentProfile)
		assert.Equal(t, "ws", string(profiles.Profiles["development"].DefaultWorkspace))
		require.NotNil(t, loadedCredentials(t, profiles, "development"))
		assert.Equal(t, endpointA, loadedCredentials(t, profiles, "development").Endpoint)
		gone := profile.Profile{Name: "dev", ConfigDir: profiles.Profiles["development"].ConfigDir}
		credentials, err := gone.Credentials(t.Context())
		require.NoError(t, err)
		assert.Nil(t, credentials.Manual)
	})

	t.Run("a new endpoint removes the credentials", func(t *testing.T) {
		moved := *profiles.Profiles["development"]
		moved.Endpoint = endpointC
		moved.Credential = credential.ManualName

		require.NoError(t, profiles.Put(t.Context(), profiles.Profiles["development"], moved))

		assert.Equal(t, endpointC, profiles.Profiles["development"].Endpoint)
		assert.Nil(t, loadedCredentials(t, profiles, "development"))
		assert.Empty(t, profiles.Profiles["development"].Credential)
	})

	t.Run("use makes a profile the current one, and refuses one there is not", func(t *testing.T) {
		require.NoError(t, profiles.SetCurrent(t.Context(), "prod"))

		assert.Equal(t, profile.Name("prod"), reloadedCurrent(t))
		require.EqualError(t, profiles.SetCurrent(t.Context(), "staging"), "there is no profile 'staging'")
	})

	t.Run("removing another profile than the current one keeps the current one", func(t *testing.T) {
		assert.Empty(t, removeWarning(t, "development"))
		assert.Equal(t, profile.Name("prod"), reloadedCurrent(t))
	})

	t.Run("removing the current profile of several left makes none the current one", func(t *testing.T) {
		require.NoError(t, profiles.Put(t.Context(), nil, profile.Profile{Name: "staging", Endpoint: endpointC}))
		require.NoError(t, profiles.Put(t.Context(), nil, profile.Profile{Name: "dev", Endpoint: endpointA}))

		assert.Contains(t, removeWarning(t, "prod"), "No profile is current now. Make one the current one in meshstack profile, "+
			"or log in to it with meshstack login --profile <name>.")
		assert.Empty(t, reloadedCurrent(t))
	})

	t.Run("removing the current profile of one left makes that the current one", func(t *testing.T) {
		require.NoError(t, profiles.SetCurrent(t.Context(), "staging"))

		assert.Contains(t, removeWarning(t, "staging"), "Profile 'dev' is the current one now, as it is the only one left.")
		assert.Equal(t, profile.Name("dev"), reloadedCurrent(t))
	})

	t.Run("removing the last profile deletes its credentials, warns of nothing, and leaves nothing to remove again", func(t *testing.T) {
		storeCredentials(t, profiles.Profiles["dev"])
		removed := *profiles.Profiles["dev"]

		assert.Empty(t, removeWarning(t, "dev"))

		assert.Empty(t, names(profiles))
		assert.Empty(t, reloadedCurrent(t))
		credentials, err := removed.Credentials(t.Context())
		require.NoError(t, err)
		assert.Nil(t, credentials.Manual)
		require.EqualError(t, remove(t.Context(), &profiles, "dev"), "there is no profile 'dev'")
	})
}

func names(profiles profile.Profiles) (names []profile.Name) {
	for _, p := range profiles.Selection().Profiles {
		names = append(names, p.Name)
	}
	return
}
