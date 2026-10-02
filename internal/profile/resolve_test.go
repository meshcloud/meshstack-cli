package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/setting/setting_test"
)

var testEndpoint = xurl.MustParsef("https://localhost:1337")

func TestResolveProfileOnAFreshConfigDirectory(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := filepath.Join(t.TempDir(), "missing")
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())
	defaultProfile := &Profile{Name: "default", Endpoint: testEndpoint}
	dev := &Profile{Name: "dev", Endpoint: testEndpoint}

	t.Run("creates the default profile, though the directory is missing", func(t *testing.T) {
		current, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

		require.NoError(t, err)
		assert.EqualExportedValues(t, defaultProfile, withoutConfigDir(current))
		assertProfiles(t, profiles, defaultProfile)
		require.NoError(t, profiles.Store(t.Context()))
	})

	t.Run("creates a missing profile the environment names, and selects it", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "dev")
		created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

		require.NoError(t, err)
		assert.EqualExportedValues(t, dev, withoutConfigDir(created))
		assertProfiles(t, profiles, dev, defaultProfile)

		created.DefaultWorkspace = "my-workspace-ab12c"
		require.NoError(t, profiles.Store(t.Context()))
	})

	t.Run("what a login stores through the returned profile survives the next run", func(t *testing.T) {
		reloaded, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

		require.NoError(t, err)
		dev.DefaultWorkspace = "my-workspace-ab12c"
		assert.EqualExportedValues(t, dev, withoutConfigDir(reloaded))
		assertProfiles(t, profiles, dev, defaultProfile)
	})

	t.Run("a profile name from a front end source wins over the environment", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "from-the-environment")

		current, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: profileNameFromFrontend("from-the-front-end")})

		require.NoError(t, err)
		assert.Equal(t, Name("from-the-front-end"), current.Name)
	})

	t.Run("a stored profile is found without its name or endpoint in the environment", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "dev-local")
		// Upper case and a trailing slash, so that the stored endpoint shows the canonical form.
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://LOCALHOST:1337/")
		devLocal := &Profile{Name: "dev-local", Endpoint: testEndpoint}
		created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
		require.NoError(t, err)
		assert.EqualExportedValues(t, devLocal, withoutConfigDir(created))
		require.NoError(t, profiles.Store(t.Context()))
		storeApiKey(t, created)

		// An empty value is skipped as no value at all, so this is the unset case.
		t.Setenv(NameSetting.EnvKey(), "")
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "")
		reloaded, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

		require.NoError(t, err)
		assert.EqualExportedValues(t, devLocal, withoutConfigDir(reloaded))
		assertApiKey(t, reloaded, "08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret")
	})

	t.Run("Add puts a profile into the config directory of the profiles", func(t *testing.T) {
		profiles, err := LoadProfiles(t.Context(), LoadProfilesOptions{})
		require.NoError(t, err)

		added := profiles.Add(Profile{Name: "dev", Endpoint: testEndpoint, ConfigDir: "elsewhere"})

		assert.Equal(t, config.Directory(configDir), added.ConfigDir)
		assert.Same(t, added, profiles.Profiles["dev"])
	})

	t.Run("a null profile is refused", func(t *testing.T) {
		require.NoError(t, os.WriteFile(config.Directory(configDir).ProfilesJson(),
			[]byte(`{"version":1,"profiles":{"broken":null}}`), 0o600))

		_, err := LoadProfiles(t.Context(), LoadProfilesOptions{})

		require.ErrorContains(t, err, "'broken'")
	})
}

// testdata/configdir is the fixture of every step, and no step changes it.
func TestResolveProfileOfAStoredConfigDirectory(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
	var asked []Selection
	selectionAnswering := func(name Name) SettingSources {
		return selectionSource(func(selection Selection) Name {
			asked = append(asked, selection)
			return name
		})
	}
	resolveWithSelection := func(t *testing.T) *Profile {
		t.Helper()
		asked = nil
		selected, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectionAnswering("default")})
		require.NoError(t, err)
		return selected
	}

	t.Run("reads the current profile and its credentials", func(t *testing.T) {
		currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

		require.NoError(t, err)
		devLocal := &Profile{Name: "dev-local", Endpoint: xurl.MustParsef("https://localhost:1337"), Credential: "apiKey"}
		assert.EqualExportedValues(t, devLocal, withoutConfigDir(currentProfile))
		assertProfiles(t, profiles,
			devLocal,
			&Profile{Name: "default", Endpoint: xurl.MustParsef("https://api.dev.meshcloud.io/")},
			&Profile{Name: "empty"},
		)
		assertApiKey(t, currentProfile, "08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret")

		// Storing what was just loaded leaves the files as they are, so testdata stays the fixture.
		require.NoError(t, profiles.Store(t.Context()))
	})

	t.Run("StoredOnly creates no profile", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "missing")

		_, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{StoredOnly: true})

		require.ErrorIs(t, err, ErrNoStoredProfile)
		require.ErrorContains(t, err, "'missing'")
		assert.NotContains(t, profiles.Profiles, Name("missing"))
	})

	t.Run("without an endpoint the selection offers every profile, ordered by name", func(t *testing.T) {
		assert.Equal(t, Name("default"), resolveWithSelection(t).Name)
		require.Len(t, asked, 1)
		assert.Nil(t, asked[0].Endpoint)
		assert.Equal(t, Name("dev-local"), asked[0].Current)
		assert.Equal(t, []Name{"default", "dev-local", "empty"}, names(asked[0].Candidates()))
	})

	t.Run("an endpoint narrows the candidates to its profiles", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://api.dev.meshcloud.io")
		resolveWithSelection(t)
		require.Len(t, asked, 1)
		assert.Equal(t, []Name{"default"}, names(asked[0].Candidates()))
		assert.Len(t, asked[0].Profiles, 3)
	})

	t.Run("an endpoint matching no profile leaves no candidate", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String()+"/other")
		resolveWithSelection(t)
		require.Len(t, asked, 1)
		assert.Empty(t, asked[0].Candidates())
	})

	t.Run("a profile named in the environment needs no selection", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "empty")
		assert.Equal(t, Name("empty"), resolveWithSelection(t).Name)
		assert.Empty(t, asked)
	})

	// This step fails where LoadProfiles resolves the profile name itself: it then reaches the
	// selection before the context carries it.
	t.Run("a first-time use offers an empty selection", func(t *testing.T) {
		t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())
		asked = nil

		created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectionAnswering("")})

		require.NoError(t, err)
		require.Len(t, asked, 1)
		assert.Empty(t, asked[0].Profiles)
		expected := &Profile{Name: "default", Endpoint: testEndpoint}
		assert.EqualExportedValues(t, expected, withoutConfigDir(created))
		assertProfiles(t, profiles, expected)
	})

	t.Run("the selection is not in the context outside the profile name resolution", func(t *testing.T) {
		_, err := SelectionFromContext(t.Context())

		require.ErrorContains(t, err, NameSetting.EnvKey())
	})
}

// givenNoMeshstackEnvironment keeps a developer's own shell out of the resolutions under test,
// which would otherwise put its endpoint into every profile created here.
func givenNoMeshstackEnvironment(t *testing.T) {
	t.Helper()
	for _, envKey := range []string{
		config.DirectorySetting.EnvKey(),
		NameSetting.EnvKey(),
		meshstack.EndpointSetting.EnvKey(),
		meshstack.WorkspaceSetting.EnvKey(),
	} {
		t.Setenv(envKey, "")
	}
}

func profileNameFromFrontend(name Name) SettingSources {
	return setting.Sources{setting.FrontendSource{Source: setting_test.LookupFunc(
		func(_ context.Context, key string) (string, error) {
			if key == NameSetting.EnvKey() {
				return string(name), nil
			}
			return "", nil
		},
	)}}
}

func storeApiKey(t *testing.T, p *Profile) {
	t.Helper()
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(&credential.ApiKey{
		Endpoint:     p.Endpoint,
		ClientId:     uuid.MustParse("08be9109-45bf-42ba-a965-2097b9d0d181"),
		ClientSecret: "super-test-secret",
	})
	require.NoError(t, creds.Store(t.Context()))
}

func assertApiKey(t *testing.T, p *Profile, clientId, clientSecret string) {
	t.Helper()
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	require.NotNil(t, creds.ApiKey)
	assert.Equal(t, uuid.MustParse(clientId), creds.ApiKey.ClientId)
	assert.Equal(t, clientSecret, creds.ApiKey.ClientSecret)
}

func assertProfiles(t *testing.T, actual Profiles, expected ...*Profile) {
	t.Helper()
	expectedProfiles := Profiles{Version: 1}
	if len(expected) > 0 {
		expectedProfiles.CurrentProfile = expected[0].Name
	}
	expectedProfiles.Profiles = make(map[Name]*Profile, len(expected))
	for _, profile := range expected {
		expectedProfiles.Profiles[profile.Name] = profile
	}
	// ConfigDir is whatever directory the run resolved, so it is asserted non-empty rather than compared.
	actualStripped := actual
	actualStripped.Profiles = make(map[Name]*Profile, len(actual.Profiles))
	for name, actualProfile := range actual.Profiles {
		assert.NotEmpty(t, actualProfile.ConfigDir)
		actualStripped.Profiles[name] = withoutConfigDir(actualProfile)
	}
	assert.EqualExportedValues(t, expectedProfiles, actualStripped)
	assert.NotEmpty(t, actual.configDir)
}

func withoutConfigDir(p *Profile) *Profile {
	stripped := *p
	stripped.ConfigDir = ""
	return &stripped
}

func selectionSource(choose func(Selection) Name) SettingSources {
	return setting.Sources{setting.FallbackSource{Source: setting.LookupSource{
		MatchingKey: NameSetting.EnvKey(),
		Description: "the profile selection",
		Func: func(ctx context.Context) (string, error) {
			selection, err := SelectionFromContext(ctx)
			if err != nil {
				return "", err
			}
			return string(choose(selection)), nil
		},
	}}}
}

func names(profiles []*Profile) []Name {
	result := make([]Name, 0, len(profiles))
	for _, p := range profiles {
		result = append(result, p.Name)
	}
	return result
}
