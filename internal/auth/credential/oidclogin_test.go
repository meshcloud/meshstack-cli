package credential

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
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
