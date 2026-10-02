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

func TestCache(t *testing.T) {
	p := &profile.Profile{
		Name:      "cache-test",
		ConfigDir: config.Directory(t.TempDir()),
		Endpoint:  xurl.MustParsef("https://localhost:1337"),
	}
	token := jsontest.MustUnmarshal[jwt.JWT](t, jwtJson)
	apiKey := func(clientId string) *credential.ApiKey {
		return &credential.ApiKey{Endpoint: p.Endpoint, ClientId: uuid.MustParse(clientId), ClientSecret: "super-test-secret"}
	}
	const stored, fromEnvironment = "11111111-45bf-42ba-a965-2097b9d0d181", "22222222-45bf-42ba-a965-2097b9d0d181"
	reloadedToken := func(t *testing.T) jwt.JWT {
		t.Helper()
		reloaded := apiKey(stored)
		require.NoError(t, auth.CacheFor(p, reloaded).Load(t.Context()))
		require.NotNil(t, reloaded.Cache)
		return reloaded.Cache.Token
	}

	t.Run("Modify stores a token after a Write that stored no cache", func(t *testing.T) {
		loggedIn := apiKey(stored)
		cache := auth.CacheFor(p, loggedIn)
		require.NoError(t, cache.Write(t.Context()))

		require.NoError(t, cache.Modify(t.Context(), func() error {
			loggedIn.Cache = withToken(token)
			return nil
		}))

		assert.Equal(t, token, reloadedToken(t))
	})

	t.Run("Modify leaves the cache of another identity alone", func(t *testing.T) {
		other := apiKey(fromEnvironment)
		require.NoError(t, auth.CacheFor(p, other).Modify(t.Context(), func() error {
			other.Cache = withToken(jwt.JWT{})
			return nil
		}))

		assert.Equal(t, token, reloadedToken(t))
	})

	t.Run("a cache of another version is ignored with a warning until a login writes it anew", func(t *testing.T) {
		loggedIn := apiKey(stored)
		path := p.ConfigDir.CredentialsCacheJsonFor(p.Name, loggedIn.Name())
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(`{"version":2,"identity":"minted by a later meshstack"}`), 0o600))
		captured := logs.Capture(t)

		require.NoError(t, auth.CacheFor(p, loggedIn).Load(t.Context()))
		assert.Nil(t, loggedIn.Cache)
		assert.Contains(t, captured.String(), "level=WARN")
		assert.Contains(t, captured.String(), "run 'meshstack login -p cache-test' to update it")

		loggedIn.Cache = withToken(token)
		require.NoError(t, auth.CacheFor(p, loggedIn).Write(t.Context()))
		assert.Equal(t, token, reloadedToken(t))
	})

	// A second browser login at the same endpoint has the identity of the first, so only the Write
	// before Modify decides whether Modify reads back the fresh refresh token or the old one.
	t.Run("Modify keeps the fresh refresh token of a second browser login", func(t *testing.T) {
		oidcLogin := func(refreshToken string) *credential.OidcLogin {
			login := &credential.OidcLogin{Endpoint: p.Endpoint, Issuer: xurl.MustParsef("https://localhost:1337/auth/realms/meshfed"), ClientId: "meshstack-cli"}
			login.StoreLogin(oidc.Token{RefreshToken: refreshToken, AccessToken: token})
			return login
		}
		require.NoError(t, auth.CacheFor(p, oidcLogin("previous-refresh-token")).Write(t.Context()))

		again := oidcLogin("fresh-refresh-token")
		cache := auth.CacheFor(p, again)
		require.NoError(t, cache.Write(t.Context()))
		require.NoError(t, cache.Modify(t.Context(), func() error { return nil }))

		assert.Equal(t, "fresh-refresh-token", again.Cache.RefreshToken)
	})
}

func withToken(token jwt.JWT) *struct {
	Token jwt.JWT `json:"token,omitzero"`
} {
	return &struct {
		Token jwt.JWT `json:"token,omitzero"`
	}{Token: token}
}
