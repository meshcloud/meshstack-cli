package profile

import (
	"context"
	_ "embed"
	"os"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/setting/setting_test"
	"github.com/meshcloud/meshstack-cli/internal/testutil/jsontest"
)

//go:embed testdata/jwt.json
var jwtJson []byte

func TestResolveProfileCreatesADefaultProfileWhenTheConfigDirectoryIsMissing(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "really-does-not-exists/and-should-never-exist/so-thats-a-unique-path")

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	expected := &Profile{Name: "default"}
	assert.EqualExportedValues(t, expected, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, expected)
}

func TestResolveProfilePrefersAProfileNameFromAFrontEndSourceOverTheEnvironment(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(NameSetting.EnvKey(), "from-the-environment")

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{
		SettingSources: profileNameFromFrontend("from-the-front-end"),
	})

	require.NoError(t, err)
	expected := &Profile{Name: "from-the-front-end"}
	assert.EqualExportedValues(t, expected, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, expected)
}

func TestResolveProfileFindsAStoredProfileWithoutItsNameOrEndpointInTheEnvironment(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(NameSetting.EnvKey(), "dev-local")
	// Upper case and a trailing slash, so that the stored endpoint shows the canonical form.
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://LOCALHOST:1337/")
	devLocal := &Profile{Name: "dev-local", Endpoint: new(xurl.MustParsef("https://localhost:%d", 1337))}
	cachedToken := jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
	require.NoError(t, err)
	assert.EqualExportedValues(t, devLocal, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles, devLocal)
	require.NoError(t, profiles.Store(t.Context()))
	storeApiKeyWithCachedToken(t, currentProfile, cachedToken)

	// An empty value is skipped as no value at all, so this is the unset case.
	t.Setenv(NameSetting.EnvKey(), "")
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "")
	reloaded, reloadedProfiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	assert.EqualExportedValues(t, devLocal, withoutConfigDir(reloaded))
	assertProfiles(t, reloadedProfiles, devLocal)
	assertApiKeyWithCachedToken(t, reloaded, "08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret", cachedToken)
}

func TestResolveProfileReadsTheCurrentProfileAndItsCachedTokenFromDisk(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")

	currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.NoError(t, err)
	devLocal := &Profile{Name: "dev-local", Endpoint: new(xurl.MustParsef("https://localhost:1337")), Credential: "apiKey"}
	assert.EqualExportedValues(t, devLocal, withoutConfigDir(currentProfile))
	assertProfiles(t, profiles,
		devLocal,
		&Profile{Name: "default", Endpoint: new(xurl.MustParsef("https://api.dev.meshcloud.io/"))},
		&Profile{Name: "empty"},
	)
	assertApiKeyWithCachedToken(t, currentProfile,
		"08be9109-45bf-42ba-a965-2097b9d0d181", "super-test-secret", jsontest.MustUnmarshal[jwt.JWT](t, jwtJson))

	// Storing what was just loaded leaves the files as they are, so testdata stays the fixture.
	require.NoError(t, profiles.Store(t.Context()))
}

func TestResolveProfileFailsOnANameThatIsNotOnDisk(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), "testdata/configdir")
	t.Setenv(NameSetting.EnvKey(), "dev-locl")

	_, _, err := ResolveProfile(t.Context(), ResolveProfileOptions{})

	require.ErrorContains(t, err, "no profile found with name dev-locl")
}

func TestResolveProfileCreatesAMissingProfileForALoginAndUpdatesItOnTheNextOne(t *testing.T) {
	givenNoMeshstackEnvironment(t)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	_, defaultOnly, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
	require.NoError(t, err)
	require.NoError(t, defaultOnly.Store(t.Context()))

	t.Setenv(NameSetting.EnvKey(), "dev")
	created, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{CreateProfileIfMissing: true})

	require.NoError(t, err)
	dev := &Profile{Name: "dev"}
	assert.EqualExportedValues(t, dev, withoutConfigDir(created))
	assertProfiles(t, profiles, dev, &Profile{Name: "default"})

	// What a login stores through the returned pointer has to survive the next one.
	created.DefaultWorkspace = "my-workspace-ab12c"
	require.NoError(t, profiles.Store(t.Context()))
	reloaded, reloadedProfiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{CreateProfileIfMissing: true})

	require.NoError(t, err)
	dev.DefaultWorkspace = "my-workspace-ab12c"
	assert.EqualExportedValues(t, dev, withoutConfigDir(reloaded))
	assertProfiles(t, reloadedProfiles, dev, &Profile{Name: "default"})
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

func storeApiKeyWithCachedToken(t *testing.T, p *Profile, token jwt.JWT) {
	t.Helper()
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.SetIdentity(&credential.ApiKey{
		Endpoint:     *p.Endpoint,
		ClientId:     uuid.MustParse("08be9109-45bf-42ba-a965-2097b9d0d181"),
		ClientSecret: "super-test-secret",
	})
	require.NoError(t, creds.Store(t.Context()))
	require.NoError(t, creds.ModifyCache(t.Context(), creds.ApiKey, func() error {
		creds.ApiKey.Cache = &struct {
			Token jwt.JWT `json:"token,omitzero"`
		}{Token: token}
		return nil
	}))
	require.NoError(t, creds.Store(t.Context()))
}

func assertApiKeyWithCachedToken(t *testing.T, p *Profile, clientId, clientSecret string, token jwt.JWT) {
	t.Helper()
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	require.NotNil(t, creds.ApiKey)
	assert.Equal(t, uuid.MustParse(clientId), creds.ApiKey.ClientId)
	assert.Equal(t, clientSecret, creds.ApiKey.ClientSecret)
	require.NoError(t, creds.ReadCache(t.Context(), creds.ApiKey, func() error {
		assert.Equal(t, token, creds.ApiKey.Cache.Token)
		return nil
	}))
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
