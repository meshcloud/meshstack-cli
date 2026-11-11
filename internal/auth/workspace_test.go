package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func TestAFrontEndThatIgnoresTheDefaultWorkspaceNeedsANamedOne(t *testing.T) {
	server := newTestServer(t)
	claims := map[string]any{
		"exp":         time.Now().Add(time.Hour).Unix(),
		"MC_CUSTOMER": "ops",
	}
	defaultWorkspaceToken := testToken(t, claims)
	storeBrowserLoginWithDefaultWorkspace(t, xurl.MustParsef("%s", server.URL), "ops", defaultWorkspaceToken)
	cliOpts := testSessionOpts // uses the default (zero) value of `IgnoreDefaultWorkspace`, which is false.
	tofuProviderOpts := testSessionOpts
	tofuProviderOpts.IgnoreDefaultWorkspace = true
	bearerToken := func(t *testing.T, opts auth.ResolveSessionOptions) (http.BearerToken, error) {
		t.Helper()
		session, err := auth.ResolveSession(t.Context(), opts)
		require.NoError(t, err)
		return session.GetBearerToken(t.Context())
	}

	t.Run("a front end such as the CLI acts in the profile's default workspace", func(t *testing.T) {
		token, err := bearerToken(t, cliOpts)
		require.NoError(t, err)
		assert.Equal(t, http.BearerToken(defaultWorkspaceToken.String()), token)
	})

	t.Run("a front end such as the tofu provider gets no token while nothing names a workspace", func(t *testing.T) {
		_, err := bearerToken(t, tofuProviderOpts)
		require.ErrorAs(t, err, &meshstack.WorkspaceNotNamedError{})
		require.ErrorContains(t, err, meshstack.WorkspaceSetting.EnvKey(), "says how to name one")
		assert.NotContains(t, err.Error(), "meshstack profile edit", "a default workspace would not help")
	})

	t.Run("it acts in the workspace that the environment names", func(t *testing.T) {
		t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "ops")
		token, err := bearerToken(t, tofuProviderOpts)
		require.NoError(t, err)
		assert.Equal(t, http.BearerToken(defaultWorkspaceToken.String()), token)
	})

	t.Run("ResolveWorkspace leaves the default out as well", func(t *testing.T) {
		workspace, err := auth.ResolveWorkspace(t.Context(), tofuProviderOpts)
		require.NoError(t, err)
		assert.Equal(t, meshstack.NoWorkspace, workspace)
	})
}

func storeBrowserLoginWithDefaultWorkspace(t *testing.T, endpoint xurl.URL, workspace meshstack.Workspace, accessToken jwt.JWT) {
	t.Helper()
	profiles, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{ExclusiveLock: true})
	require.NoError(t, err)
	require.NoError(t, profiles.Put(t.Context(), nil, profile.Profile{
		Name:             "dev",
		Endpoint:         endpoint,
		DefaultWorkspace: workspace,
		Credential:       credential.OidcLoginName,
	}))
	require.NoError(t, profiles.Unlock())
	p := profiles.Profiles["dev"]
	login := &credential.OidcLogin{
		Endpoint: endpoint,
		Issuer:   xurl.MustParsef("https://localhost:1/realms/meshfed"),
		ClientId: "meshstack-cli",
	}
	login.StoreLogin(oidc.Token{
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: time.Now().Add(time.Hour),
		AccessToken:      accessToken,
	})
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(login)
	require.NoError(t, creds.Store(t.Context()))
	require.NoError(t, auth.CacheFor(p, login).Write(t.Context()))
}
