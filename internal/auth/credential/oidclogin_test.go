package credential

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
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
