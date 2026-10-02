package auth_test

import (
	_ "embed"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/jsontest"
)

//go:embed testdata/jwt.json
var jwtJson []byte

func TestCacheModifyLeavesACacheOfAnotherIdentityAlone(t *testing.T) {
	p := newCacheTestProfile(t)
	stored := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	stored.Cache = &struct {
		Token jwt.JWT `json:"token,omitzero"`
	}{Token: jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)}
	require.NoError(t, auth.CacheFor(p, stored).Write(t.Context()))

	fromEnvironment := newCacheTestApiKey(p, "22222222-45bf-42ba-a965-2097b9d0d181")
	require.NoError(t, auth.CacheFor(p, fromEnvironment).Modify(t.Context(), func() error {
		fromEnvironment.Cache = &struct {
			Token jwt.JWT `json:"token,omitzero"`
		}{}
		return nil
	}))

	reloaded := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	require.NoError(t, auth.CacheFor(p, reloaded).Load(t.Context()))
	require.NotNil(t, reloaded.Cache)
	assert.Equal(t, stored.Cache.Token, reloaded.Cache.Token)
}

func TestCacheModifyWritesBackOverAWriteOfNoCacheYet(t *testing.T) {
	p := newCacheTestProfile(t)
	loggedIn := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	cache := auth.CacheFor(p, loggedIn)
	require.NoError(t, cache.Write(t.Context()))

	token := jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)
	require.NoError(t, cache.Modify(t.Context(), func() error {
		loggedIn.Cache = &struct {
			Token jwt.JWT `json:"token,omitzero"`
		}{Token: token}
		return nil
	}))

	reloaded := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	require.NoError(t, auth.CacheFor(p, reloaded).Load(t.Context()))
	require.NotNil(t, reloaded.Cache)
	assert.Equal(t, token, reloaded.Cache.Token)
}

// A browser login again at the same endpoint has the identity of the previous one, so only the
// Write before it decides whether Modify reads back the fresh refresh token or the old one.
func TestCacheModifyKeepsTheRefreshTokenOfALoginAgain(t *testing.T) {
	p := newCacheTestProfile(t)
	token := jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)
	previous := newCacheTestOidcLogin(p)
	previous.StoreLogin(oidc.Token{RefreshToken: "previous-refresh-token", AccessToken: token})
	require.NoError(t, auth.CacheFor(p, previous).Write(t.Context()))

	again := newCacheTestOidcLogin(p)
	again.StoreLogin(oidc.Token{RefreshToken: "fresh-refresh-token", AccessToken: token})
	cache := auth.CacheFor(p, again)
	require.NoError(t, cache.Write(t.Context()))
	require.NoError(t, cache.Modify(t.Context(), func() error { return nil }))

	assert.Equal(t, "fresh-refresh-token", again.Cache.RefreshToken)
}

func TestCacheOfAnotherVersionIsIgnoredWithAWarningUntilALoginWritesItAnew(t *testing.T) {
	p := newCacheTestProfile(t)
	loggedIn := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	path := p.ConfigDir.CredentialsCacheJsonFor(p.Name, loggedIn.Name())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"version":2,"identity":"minted by a later meshstack"}`), 0o600))
	captured := logs.Capture(t)

	require.NoError(t, auth.CacheFor(p, loggedIn).Load(t.Context()))
	assert.Nil(t, loggedIn.Cache)
	assert.Contains(t, captured.String(), "level=WARN")
	assert.Contains(t, captured.String(), "run 'meshstack login -p cache-test' to update it")

	loggedIn.Cache = &struct {
		Token jwt.JWT `json:"token,omitzero"`
	}{Token: jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)}
	require.NoError(t, auth.CacheFor(p, loggedIn).Write(t.Context()))
	reloaded := newCacheTestApiKey(p, "11111111-45bf-42ba-a965-2097b9d0d181")
	require.NoError(t, auth.CacheFor(p, reloaded).Load(t.Context()))
	require.NotNil(t, reloaded.Cache)
	assert.Equal(t, loggedIn.Cache.Token, reloaded.Cache.Token)
}

func newCacheTestProfile(t *testing.T) *profile.Profile {
	t.Helper()
	return &profile.Profile{
		Name:      "cache-test",
		ConfigDir: config.Directory(t.TempDir()),
		Endpoint:  xurl.MustParsef("https://localhost:1337"),
	}
}

func newCacheTestApiKey(p *profile.Profile, clientId string) *credential.ApiKey {
	return &credential.ApiKey{
		Endpoint:     p.Endpoint,
		ClientId:     uuid.MustParse(clientId),
		ClientSecret: "super-test-secret",
	}
}

func newCacheTestOidcLogin(p *profile.Profile) *credential.OidcLogin {
	return &credential.OidcLogin{
		Endpoint: p.Endpoint,
		Issuer:   xurl.MustParsef("https://localhost:1337/auth/realms/meshfed"),
		ClientId: "meshstack-cli",
	}
}
