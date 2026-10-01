package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
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
	profiles, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
	require.NoError(t, err)
	return profiles
}

func storedProfiles(t *testing.T, stored ...profile.Profile) profile.Profiles {
	t.Helper()
	profiles := emptyConfigDir(t)
	for _, p := range stored {
		require.NoError(t, put(t.Context(), &profiles, nil, p))
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

func TestPutAddsOrReplacesAProfile(t *testing.T) {
	t.Run("the first profile added becomes the current one", func(t *testing.T) {
		profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA}, profile.Profile{Name: "prod", Endpoint: endpointB})

		reloaded, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
		require.NoError(t, err)
		assert.Equal(t, profile.Name("dev"), reloaded.CurrentProfile)
		assert.Equal(t, []profile.Name{"dev", "prod"}, names(reloaded))
		assert.NotEmpty(t, profiles.Profiles["prod"].ConfigDir, "an added profile knows its configuration directory")
	})

	t.Run("a name that is taken is refused", func(t *testing.T) {
		profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA}, profile.Profile{Name: "prod", Endpoint: endpointB})

		require.EqualError(t, put(t.Context(), &profiles, nil, profile.Profile{Name: "dev", Endpoint: endpointB}),
			"a profile named 'dev' exists already")
		require.EqualError(t, put(t.Context(), &profiles, profiles.Profiles["prod"], profile.Profile{Name: "dev", Endpoint: endpointB}),
			"a profile named 'dev' exists already")
	})

	t.Run("a rename takes the credentials and the current profile along", func(t *testing.T) {
		profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA, DefaultWorkspace: "ws"})
		storeCredentials(t, profiles.Profiles["dev"])
		renamed := *profiles.Profiles["dev"]
		renamed.Name = "development"

		require.NoError(t, put(t.Context(), &profiles, profiles.Profiles["dev"], renamed))

		assert.Equal(t, []profile.Name{"development"}, names(profiles))
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
		profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA})
		storeCredentials(t, profiles.Profiles["dev"])
		moved := *profiles.Profiles["dev"]
		moved.Endpoint = endpointB

		require.NoError(t, put(t.Context(), &profiles, profiles.Profiles["dev"], moved))

		assert.Equal(t, endpointB, profiles.Profiles["dev"].Endpoint)
		assert.Nil(t, loadedCredentials(t, profiles, "dev"))
	})
}

func TestRemoveDeletesTheProfileWithItsCredentials(t *testing.T) {
	profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA}, profile.Profile{Name: "prod", Endpoint: endpointB})
	storeCredentials(t, profiles.Profiles["dev"])
	removed := *profiles.Profiles["dev"]

	require.NoError(t, remove(t.Context(), &profiles, "dev"))

	assert.Equal(t, []profile.Name{"prod"}, names(profiles))
	assert.Empty(t, profiles.CurrentProfile, "the current profile went with it")
	credentials, err := removed.Credentials(t.Context())
	require.NoError(t, err)
	assert.Nil(t, credentials.Manual)
	require.EqualError(t, remove(t.Context(), &profiles, "dev"), "there is no profile 'dev'")
}

func TestUseMakesAProfileTheCurrentOne(t *testing.T) {
	profiles := storedProfiles(t, profile.Profile{Name: "dev", Endpoint: endpointA}, profile.Profile{Name: "prod", Endpoint: endpointB})

	require.NoError(t, use(t.Context(), &profiles, "prod"))

	reloaded, err := profile.LoadProfiles(t.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
	require.NoError(t, err)
	assert.Equal(t, profile.Name("prod"), reloaded.CurrentProfile)
	require.EqualError(t, use(t.Context(), &profiles, "staging"), "there is no profile 'staging'")
}

func names(profiles profile.Profiles) (names []profile.Name) {
	for _, p := range profiles.Selection().Profiles {
		names = append(names, p.Name)
	}
	return
}
