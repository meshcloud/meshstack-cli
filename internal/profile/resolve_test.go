package profile

import (
	_ "embed"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/setting/setting_test"
)

var (
	//go:embed testdata/jwt.json
	jwtJson []byte
)

func TestResolveProfile(t *testing.T) {

	t.Run("init from non-existing config dir", func(t *testing.T) {
		t.Setenv(ConfigDirectorySetting.EnvKey(), "really-does-not-exists/and-should-never-exist/so-thats-a-unique-path")
		currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
		require.NoError(t, err)
		expectedDefaultProfile := &Profile{Name: "default"}
		assert.EqualExportedValues(t, expectedDefaultProfile, currentProfile)
		assertProfiles(t, profiles, expectedDefaultProfile)
	})

	t.Run("with some name in env and default config dir", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(ConfigDirectorySetting.EnvKey(), tempDir)
		t.Setenv(NameSetting.EnvKey(), "ignored-because-explicit-source-is-active")

		expectedSomeProfile := &Profile{Name: "some-name"}
		profile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{
			// Use ExplicitSource instead of environment just to cover that in test as well here
			UseSettingsFrom: []setting.ExplicitSource{{Source: setting_test.LookupFunc(func(key string) (string, error) {
				if key == NameSetting.EnvKey() {
					return string(expectedSomeProfile.Name), nil
				}
				return "", nil
			})}},
		})
		require.NoError(t, err)
		assert.EqualExportedValues(t, expectedSomeProfile, profile)
		assertProfiles(t, profiles, expectedSomeProfile)
	})

	expectedDevLocalProfile := &Profile{Name: "dev-local", Endpoint: new(xurl.MustParsef("https://localhost:%d", 1337))}

	t.Run("init from temp writable config dir", func(t *testing.T) {
		t.Setenv(ConfigDirectorySetting.EnvKey(), t.TempDir())

		t.Run("with dev-local name and endpoint in env", func(t *testing.T) {
			t.Setenv(NameSetting.EnvKey(), expectedDevLocalProfile.String())
			t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://LOCALHOST:1337/") // no trailing / to test canonicalization during load/store

			profile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
			require.NoError(t, err)
			assert.EqualExportedValues(t, expectedDevLocalProfile, profile)
			assertProfiles(t, profiles, expectedDevLocalProfile)
			require.NoError(t, profiles.Store(t.Context()))

			t.Run("resolve apikey credentials", func(t *testing.T) {
				creds, err := profile.Credentials(t.Context())
				require.NoError(t, err)
				creds.SetIdentity(&credential.ApiKey{
					Endpoint:     *profile.Endpoint,
					ClientId:     uuid.MustParse("08be9109-45bf-42ba-a965-2097b9d0d181"),
					ClientSecret: "super-test-secret",
				})
				require.NoError(t, creds.Store(t.Context()))

				t.Run("modify apikey cached token", func(t *testing.T) {
					require.NoError(t, creds.ModifyCache(t.Context(), creds.ApiKey, func() error {
						creds.ApiKey.Cache = &struct {
							Token jwt.JWT `json:"token,omitzero"`
						}{Token: json.MustUnmarshal[jwt.JWT](t, jwtJson)}
						return nil
					}))
					require.NoError(t, creds.Store(t.Context()))
				})
			})
		})

		t.Run("load again without env", func(t *testing.T) {
			profile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
			require.NoError(t, err)
			assert.EqualExportedValues(t, expectedDevLocalProfile, profile)
			assertProfiles(t, profiles, expectedDevLocalProfile)
			t.Run("load/assert stored apikey cred with cache", func(t *testing.T) {
				creds, err := profile.Credentials(t.Context())
				require.NoError(t, err)
				require.NotNil(t, creds.ApiKey)
				assert.NotEmpty(t, creds.ApiKey.ClientId)
				assert.NotEmpty(t, creds.ApiKey.ClientSecret)
				require.NoError(t, creds.ReadCache(t.Context(), creds.ApiKey, func() error {
					assert.Equal(t, json.MustUnmarshal[jwt.JWT](t, jwtJson), creds.ApiKey.Cache.Token)
					return nil
				}))
			})
		})
	})

	t.Run("load testdata/configdir", func(t *testing.T) {
		t.Setenv(ConfigDirectorySetting.EnvKey(), "testdata/configdir")
		currentProfile, profiles, err := ResolveProfile(t.Context(), ResolveProfileOptions{})
		require.NoError(t, err)
		expectedLoggedInDevLocalProfile := &Profile{Name: "dev-local", Endpoint: expectedDevLocalProfile.Endpoint, Credential: "apiKey"}
		assert.EqualExportedValues(t, expectedLoggedInDevLocalProfile, currentProfile)
		expectedDefaultProfile := &Profile{Name: "default", Endpoint: new(xurl.MustParsef("https://api.dev.meshcloud.io/"))}
		expectedEmptyProfile := &Profile{Name: "empty"}
		assertProfiles(t, profiles,
			expectedLoggedInDevLocalProfile,
			expectedDefaultProfile,
			expectedEmptyProfile,
		)
		require.NoError(t, profiles.Store(t.Context()))

		t.Run("load stored apikey cred with cache", func(t *testing.T) {
			creds, err := currentProfile.Credentials(t.Context())
			require.NoError(t, err)
			require.NotNil(t, creds.ApiKey)
			assert.Equal(t, uuid.MustParse("08be9109-45bf-42ba-a965-2097b9d0d181"), creds.ApiKey.ClientId)
			assert.Equal(t, "super-test-secret", creds.ApiKey.ClientSecret)
			require.NoError(t, creds.ReadCache(t.Context(), creds.ApiKey, func() error {
				assert.Equal(t, json.MustUnmarshal[jwt.JWT](t, jwtJson), creds.ApiKey.Cache.Token)
				return nil
			}))
			require.NoError(t, creds.Store(t.Context()))
		})
	})
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
	assert.EqualExportedValues(t, expectedProfiles, actual)

	// extra care that unexported configDir is non-empty in actual
	assert.NotEmpty(t, actual.configDir)
	for _, actualProfile := range actual.Profiles {
		assert.NotEmpty(t, actualProfile.configDir)
	}
}
