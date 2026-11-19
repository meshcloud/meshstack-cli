package credential

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

func TestTheWorkspaceDecidesTheCacheKeyAndTheScopesAsked(t *testing.T) {
	for _, test := range []struct {
		name      string
		workspace meshstack.Workspace
		cacheKey  string
		scopes    string
	}{
		{
			name:      "a workspace",
			workspace: "demo-partner",
			cacheKey:  "c:demo-partner",
			scopes:    "openid c:demo-partner",
		},
		{
			name:      "no workspace",
			workspace: meshstack.NoWorkspace,
			cacheKey:  "unscoped",
			scopes:    "openid offline_access",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.cacheKey, string(tokenCacheKey(test.workspace)))
			assert.Equal(t, test.scopes, scopesFor(test.workspace).String())
		})
	}
}

func TestALoginKeepsTheSessionEndOfItsLatestToken(t *testing.T) {
	login := &OidcLogin{}
	sessionEnd := time.Date(2026, 9, 30, 10, 57, 0, 0, time.UTC)

	login.StoreLogin(oidc.Token{RefreshToken: "first", RefreshExpiresAt: sessionEnd})
	assert.Equal(t, sessionEnd, login.Cache.RefreshExpiresAt)

	login.StoreLogin(oidc.Token{RefreshToken: "rotated"})
	assert.True(t, login.Cache.RefreshExpiresAt.IsZero(), "a refresh that names no limit leaves none")
}

func TestThePreviousAccessLevelIsTheOneItsLatestTokenGrants(t *testing.T) {
	tokenWith := func(t *testing.T, expiry int64, scopes string) jwt.JWT {
		t.Helper()
		claims := fmt.Sprintf(`{"exp":%d,"scope":%q}`, expiry, scopes)
		var token jwt.JWT
		require.NoError(t, token.UnmarshalText([]byte("e30."+base64.RawURLEncoding.EncodeToString([]byte(claims))+".sig")))
		return token
	}
	login := &OidcLogin{}

	t.Run("a login without tokens keeps the stored level", func(t *testing.T) {
		login.AccessLevel = meshstack.AccessRead
		assert.Equal(t, meshstack.AccessRead, login.GrantedLevel())
	})

	t.Run("a login stored as full access that its tokens grant safe access is safe access", func(t *testing.T) {
		login.AccessLevel = ""
		login.StoreLogin(oidc.Token{AccessToken: tokenWith(t, 1000, "openid cli-access-write")})
		assert.Equal(t, meshstack.AccessWrite, login.GrantedLevel())
	})

	t.Run("the latest token decides", func(t *testing.T) {
		login.Cache.ScopedTokens["c:demo"] = tokenWith(t, 2000, "openid cli-access-full")
		assert.Equal(t, meshstack.AccessFull, login.GrantedLevel())
	})

	t.Run("a meshStack without access levels keeps the stored level", func(t *testing.T) {
		login.Cache.ScopedTokens["c:demo"] = tokenWith(t, 3000, "openid email")
		assert.Empty(t, login.GrantedLevel())
	})
}
