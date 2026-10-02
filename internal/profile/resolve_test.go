package profile

import (
	"context"
	"os"
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

func TestResolveProfileCreatesADefaultProfileWhenTheConfigDirectoryIsMissing(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "really-does-not-exists/and-should-never-exist/so-thats-a-unique-path")
	t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	expected := &Profile{Name: "default", Endpoint: testEndpoint}
	assert.EqualExportedValues(t, expected, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, expected)
}

func TestResolveProfilePrefersAProfileNameFromAFrontEndSourceOverTheEnvironment(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(NameSetting.EnvKey(), "from-the-environment")
	t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{
		SettingSources: profileNameFromFrontend("from-the-front-end"),
	})

	require.NoError(t, err)
	expected := &Profile{Name: "from-the-front-end", Endpoint: testEndpoint}
	assert.EqualExportedValues(t, expected, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, expected)
}

func TestResolveProfileFindsAStoredProfileWithoutItsNameOrEndpointInTheEnvironment(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(NameSetting.EnvKey(), "dev-local")
	// Upper case and a trailing slash, so that the stored endpoint shows the canonical form.
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://LOCALHOST:1337/")
	devLocal := &Profile{Name: "dev-local", Endpoint: xurl.MustParsef("https://localhost:%d", 1337)}

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
	require.NoError(t, err)
	assert.EqualExportedValues(t, devLocal, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, devLocal)
	require.NoError(t, profiles.Store(t.Context()))
	storeApiKey(t, currentProfile)

	// An empty value is skipped as no value at all, so this is the unset case.
	t.Setenv(NameSetting.EnvKey(), "")
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "")
	reloaded, reloadedProfiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	assert.EqualExportedValues(t, devLocal, withoutConfigDir(reloaded))
	assertProfiles(t, reloadedProfiles, devLocal)
	assertApiKey(t, reloaded, "08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret")
}

func TestResolveProfileReadsTheCurrentProfileAndItsCredentialsFromDisk(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")

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
}

func TestResolveProfileCreatesAMissingProfileAndUpdatesItOnTheNextRun(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())
	_, defaultOnly, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
	require.NoError(t, err)
	require.NoError(t, defaultOnly.Store(t.Context()))

	t.Setenv(NameSetting.EnvKey(), "dev")
	created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	dev := &Profile{Name: "dev", Endpoint: testEndpoint}
	assert.EqualExportedValues(t, dev, withoutConfigDir(created))
	assertProfiles(t, profiles, dev, &Profile{Name: "default", Endpoint: testEndpoint})

	// What a login stores through the returned pointer has to survive the next one.
	created.DefaultWorkspace = "my-workspace-ab12c"
	require.NoError(t, profiles.Store(t.Context()))
	reloaded, reloadedProfiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	dev.DefaultWorkspace = "my-workspace-ab12c"
	assert.EqualExportedValues(t, dev, withoutConfigDir(reloaded))
	assertProfiles(t, reloadedProfiles, dev, &Profile{Name: "default", Endpoint: testEndpoint})
}

func TestResolveProfileOfStoredOnlyCreatesNoProfile(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
	t.Setenv(NameSetting.EnvKey(), "missing")

	_, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{StoredOnly: true})

	require.ErrorIs(t, err, ErrNoStoredProfile)
	require.ErrorContains(t, err, "'missing'")
	assert.NotContains(t, profiles.Profiles, Name("missing"))
}

func TestLoadProfilesRejectsANullProfile(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	require.NoError(t, os.WriteFile(config.Directory(configDir).ProfilesJson(),
		[]byte(`{"version":1,"profiles":{"broken":null}}`), 0o600))

	_, err := LoadProfiles(t.Context(), ResolveProfileOptions{})

	require.ErrorContains(t, err, "'broken'")
}

func TestAddPutsAProfileIntoTheConfigDirectoryOfTheProfiles(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := t.TempDir()
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	profiles, err := LoadProfiles(t.Context(), ResolveProfileOptions{})
	require.NoError(t, err)

	added := Add(&profiles, Profile{Name: "dev", Endpoint: testEndpoint, ConfigDir: "elsewhere"})

	assert.Equal(t, config.Directory(configDir), added.ConfigDir)
	assert.Same(t, added, profiles.Profiles["dev"])
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

func TestResolveProfileOffersASelectionOnlyWhenNothingElseNamesTheProfile(t *testing.T) {
	var asked []Selection
	selectDefault := selectionSource(func(selection Selection) Name {
		asked = append(asked, selection)
		return "default"
	})

	t.Run("without an endpoint it offers every profile, ordered by name", func(t *testing.T) {
		givenNoMeshstackEnvironment(t)
		t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
		asked = nil

		selected, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectDefault})

		require.NoError(t, err)
		assert.Equal(t, Name("default"), selected.Name)
		require.Len(t, asked, 1)
		assert.Nil(t, asked[0].Endpoint)
		assert.Equal(t, Name("dev-local"), asked[0].Current)
		assert.Equal(t, []Name{"default", "dev-local", "empty"}, names(asked[0].Candidates()))
	})

	t.Run("an endpoint narrows the candidates to its profiles", func(t *testing.T) {
		givenNoMeshstackEnvironment(t)
		t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://api.dev.meshcloud.io")
		asked = nil

		_, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectDefault})

		require.NoError(t, err)
		require.Len(t, asked, 1)
		assert.Equal(t, []Name{"default"}, names(asked[0].Candidates()))
		assert.Len(t, asked[0].Profiles, 3)
	})

	t.Run("an endpoint matching no profile leaves no candidate", func(t *testing.T) {
		givenNoMeshstackEnvironment(t)
		t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String()+"/other")
		asked = nil

		_, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectDefault})

		require.NoError(t, err)
		require.Len(t, asked, 1)
		assert.Empty(t, asked[0].Candidates())
	})

	t.Run("a profile named in the environment needs no selection", func(t *testing.T) {
		givenNoMeshstackEnvironment(t)
		t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
		t.Setenv(NameSetting.EnvKey(), "empty")
		asked = nil

		selected, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: selectDefault})

		require.NoError(t, err)
		assert.Equal(t, Name("empty"), selected.Name)
		assert.Empty(t, asked)
	})

	// A LoadProfiles that resolves the profile name itself reaches the selection before the
	// context carries it.
	t.Run("a first-time use offers an empty selection", func(t *testing.T) {
		givenNoMeshstackEnvironment(t)
		t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())
		asked = nil
		leaveItToTheDefault := selectionSource(func(selection Selection) Name {
			asked = append(asked, selection)
			return ""
		})

		created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{SettingSources: leaveItToTheDefault})

		require.NoError(t, err)
		require.Len(t, asked, 1)
		assert.Empty(t, asked[0].Profiles)
		expected := &Profile{Name: "default", Endpoint: testEndpoint}
		assert.EqualExportedValues(t, expected, withoutConfigDir(created))
		assertProfiles(t, profiles, expected)
	})
}

func TestSelectionFromContextFailsOutsideTheProfileNameResolution(t *testing.T) {
	_, err := SelectionFromContext(t.Context())

	require.ErrorContains(t, err, NameSetting.EnvKey())
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
