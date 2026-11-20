package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/setting/setting_test"
)

var testEndpoint = xurl.MustParsef("https://localhost:1337")

func TestALoginCreatesItsProfile(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	configDir := filepath.Join(t.TempDir(), "missing")
	t.Setenv(config.DirectorySetting.EnvKey(), configDir)
	resolveForLogin := func(t *testing.T) (*Profile, Profiles, error) {
		t.Helper()
		profiles, err := LoadProfiles(t.Context(), LoadProfilesOptions{})
		require.NoError(t, err)
		resolved, err := profiles.Resolve(t.Context(), nil, true)
		return resolved, profiles, err
	}

	t.Run("a new profile needs an endpoint", func(t *testing.T) {
		_, _, err := resolveForLogin(t)

		require.ErrorIs(t, err, setting.ErrNoSourceProvidedValue)
		require.ErrorContains(t, err, "the new profile 'default' needs an endpoint")
	})

	t.Run("without a name it is the profile default, though the directory is missing", func(t *testing.T) {
		// Upper case and a trailing slash, so that the stored endpoint shows the canonical form.
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://LOCALHOST:1337/")
		created, profiles, err := resolveForLogin(t)

		require.NoError(t, err)
		assert.EqualExportedValues(t, &Profile{Name: "default", Endpoint: testEndpoint}, withoutConfigDir(created))
		assert.Same(t, created, profiles.Profiles["default"])
		assert.Equal(t, config.Directory(configDir), created.ConfigDir)
		require.NoError(t, profiles.SetCurrent(t.Context(), created.Name))
	})

	t.Run("a name in the environment creates a second profile next to the stored one", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String())
		t.Setenv(NameSetting.EnvKey(), "dev")
		created, profiles, err := resolveForLogin(t)

		require.NoError(t, err)
		assert.Equal(t, Name("dev"), created.Name)
		assert.Len(t, profiles.Profiles, 2)
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
		selected, _, err := ResolveProfile(t.Context(), selectionAnswering("default"))
		require.NoError(t, err)
		return selected
	}

	t.Run("without a name or an endpoint it is the current profile, with its credentials", func(t *testing.T) {
		currentProfile, profiles, err := ResolveProfile(t.Context(), nil)

		require.NoError(t, err)
		devLocal := &Profile{Name: "dev-local", Endpoint: testEndpoint, Credential: "apiKey"}
		assert.EqualExportedValues(t, devLocal, withoutConfigDir(currentProfile))
		assert.Len(t, profiles.Profiles, 3)
		assertApiKey(t, currentProfile, "08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret")
	})

	t.Run("an unknown name is an error", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "typo")

		_, _, err := ResolveProfile(t.Context(), nil)

		require.ErrorContains(t, err, "there is no profile 'typo'; run 'meshstack login --profile typo' to create it")
	})

	t.Run("a name from a front end source wins over the environment", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "dev-local")

		current, _, err := ResolveProfile(t.Context(), profileNameFromFrontend("default"))

		require.NoError(t, err)
		assert.Equal(t, Name("default"), current.Name)
	})

	t.Run("a named profile must have the endpoint given", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "dev-local")
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://api.dev.meshcloud.io")

		_, _, err := ResolveProfile(t.Context(), nil)

		require.ErrorContains(t, err, "profile 'dev-local' has the endpoint https://localhost:1337, which does not match the endpoint https://api.dev.meshcloud.io")
	})

	t.Run("an endpoint of another profile than the current one takes the only profile that has it", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://API.dev.meshcloud.io/")

		resolved, _, err := ResolveProfile(t.Context(), nil)

		require.NoError(t, err)
		assert.Equal(t, Name("default"), resolved.Name)
	})

	t.Run("an endpoint of no stored profile gives a nameless profile, which is not stored", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://localhost:4711")

		nameless, profiles, err := ResolveProfile(t.Context(), nil)

		require.NoError(t, err)
		assert.EqualExportedValues(t, &Profile{Endpoint: xurl.MustParsef("https://localhost:4711")}, withoutConfigDir(nameless))
		assert.Len(t, profiles.Profiles, 3)
		assert.Equal(t, "meshstack login --endpoint https://localhost:4711", nameless.LoginHint())
	})

	t.Run("an endpoint with a path is refused", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), testEndpoint.String()+"/api")

		_, _, err := ResolveProfile(t.Context(), nil)

		require.ErrorContains(t, err, "an endpoint is a scheme and a host")
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

	t.Run("a profile named in the environment needs no selection", func(t *testing.T) {
		t.Setenv(NameSetting.EnvKey(), "empty")
		assert.Equal(t, Name("empty"), resolveWithSelection(t).Name)
		assert.Empty(t, asked)
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

func assertApiKey(t *testing.T, p *Profile, clientId, clientSecret string) {
	t.Helper()
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	require.NotNil(t, creds.ApiKey)
	assert.Equal(t, clientId, creds.ApiKey.ClientId)
	assert.Equal(t, clientSecret, creds.ApiKey.ClientSecret)
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
