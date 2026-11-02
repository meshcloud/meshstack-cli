package auth_test

import (
	"encoding/base64"
	"encoding/json/v2"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/logs"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func TestStatusOfABrowserLogin(t *testing.T) {
	newTestServer(t)
	sessionEnd := time.Now().Add(20 * time.Hour).Truncate(time.Second)
	tokenExpiry := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	// Nothing listens on the issuer, so a status that called it would fail.
	p := storedOidcLogin(t, xurl.MustParsef("https://localhost:1/realms/meshfed"), "", oidc.Token{
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: sessionEnd,
		AccessToken: testToken(t, map[string]any{
			"exp": tokenExpiry.Unix(), "scope": "openid cli-access-read offline_access",
			"preferred_username": "jane", "email": "jane@example.com", "MC_CUSTOMER": "ops",
		}),
	})
	status := func(t *testing.T) auth.CredentialStatus {
		t.Helper()
		session, err := auth.StoredSession(t.Context(), p, testSessionOpts)
		require.NoError(t, err)
		return session.Status(t.Context())
	}

	t.Run("is read from the cache alone", func(t *testing.T) {
		status := status(t)

		assert.Equal(t, credential.OidcLoginName, status.Kind)
		assert.Equal(t, []string{"file " + p.ConfigDir.CredentialsJsonFor(p.Name)}, status.Sources)
		assert.Equal(t, meshstack.Workspace("ops"), status.Workspace)
		require.NotNil(t, status.OidcLogin)
		assert.Equal(t, meshstack.AccessFull, status.OidcLogin.AccessLevel, "a login of an older CLI stored no level, and had full access")
		assert.True(t, sessionEnd.Equal(status.OidcLogin.SessionEndsAt))
		assert.Equal(t, &auth.TokenStatus{
			ExpiresAt: tokenExpiry, User: "jane", Email: "jane@example.com", Workspace: "ops", AccessLevel: meshstack.AccessRead,
		}, status.Token)
		assert.Nil(t, status.ApiKey)
		assert.Empty(t, status.Unused, "the only stored credential is the one in use")
	})

	t.Run("scopes the session to the profile and the workspace of the login", func(t *testing.T) {
		session, err := auth.StoredSession(t.Context(), p, testSessionOpts)
		require.NoError(t, err)

		assert.Equal(t, "profile dev working in workspace ops", session.Scope())
	})

	t.Run("lists the stored credentials besides the one in use", func(t *testing.T) {
		creds, err := p.Credentials(t.Context())
		require.NoError(t, err)
		creds.Set(&credential.ApiKey{Endpoint: p.Endpoint, ClientId: uuid.MustParse(testApiKey2.ClientId), ClientSecret: testApiKey2.ClientSecret})
		require.NoError(t, creds.Store(t.Context()))

		assert.Equal(t, []auth.UnusedCredential{{Kind: credential.ApiKeyName, ClientId: testApiKey2.ClientId}}, status(t).Unused)
	})
}

func TestStatusOfAnApiKeyIsReadFromFilesAndTheEnvironment(t *testing.T) {
	newTestServer(t)
	setApiKeyEnv(t, testApiKey1)
	captured := logs.Capture(t)

	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	status := session.Status(t.Context())

	assert.Equal(t, []string{"env MESHSTACK_API_KEY", "env MESHSTACK_API_SECRET"}, status.Sources)
	require.NotNil(t, status.ApiKey)
	assert.Equal(t, testApiKey1.ClientId, status.ApiKey.ClientId.String())
	assert.Nil(t, status.Token, "no token is cached before the key is first used")
	assert.Empty(t, captured.Records(slog.LevelWarn))
}

func TestStatusOfAnApiToken(t *testing.T) {
	newTestServer(t)
	statusWithTokenInTheEnvironment := func(t *testing.T, claims map[string]any) auth.CredentialStatus {
		t.Helper()
		t.Setenv(auth.ApiTokenSetting.EnvKey(), testToken(t, claims).String())
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		return session.Status(t.Context())
	}
	validFor := func(d time.Duration) int64 { return time.Now().Add(d).Unix() }
	t.Setenv(auth.ApiTokenSetting.EnvKey(), testToken(t, map[string]any{"exp": validFor(time.Hour), "preferred_username": "stored"}).String())
	storeLogin(t, credential.ManualName)

	t.Run("a token from the environment shows the stored one as unused", func(t *testing.T) {
		status := statusWithTokenInTheEnvironment(t, map[string]any{"exp": validFor(time.Hour), "preferred_username": "env"})

		assert.Equal(t, []string{"env MESHSTACK_API_TOKEN"}, status.Sources)
		require.NotNil(t, status.Token)
		assert.Equal(t, "env", status.Token.User)
		require.Len(t, status.Unused, 1)
		assert.True(t, status.Unused[0].Selected)
		require.NotNil(t, status.Unused[0].Token)
		assert.Equal(t, "stored", status.Unused[0].Token.User)
	})

	t.Run("the token of a building block run shows the client id of its API key", func(t *testing.T) {
		status := statusWithTokenInTheEnvironment(t, map[string]any{"exp": validFor(time.Hour), "client_id": testApiKey1.ClientId})

		assert.Equal(t, credential.ManualName, status.Kind)
		require.NotNil(t, status.Token)
		assert.Equal(t, testApiKey1.ClientId, status.Token.ClientId)
	})

	t.Run("an expired token says so", func(t *testing.T) {
		captured := logs.Capture(t)
		status := statusWithTokenInTheEnvironment(t, map[string]any{"exp": validFor(-time.Hour), "client_id": testApiKey1.ClientId})

		require.NotNil(t, status.Token)
		assert.True(t, status.Token.Expired())
		assert.Equal(t, testApiKey1.ClientId, status.Token.ClientId)
		warnings := captured.Lines(slog.LevelWarn)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "set a credential over 'manual' stored in profile 'default'")
	})
}

func storedOidcLogin(t *testing.T, issuer xurl.URL, level meshstack.AccessLevel, token oidc.Token) *profile.Profile {
	t.Helper()
	p := &profile.Profile{
		Name:             "dev",
		Endpoint:         xurl.MustParsef("https://localhost:1"),
		DefaultWorkspace: "ops",
		Credential:       credential.OidcLoginName,
		ConfigDir:        config.Directory(t.TempDir()),
	}
	login := &credential.OidcLogin{Endpoint: p.Endpoint, Issuer: issuer, ClientId: "meshstack-cli", AccessLevel: level}
	login.StoreLogin(token)
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(login)
	require.NoError(t, creds.Store(t.Context()))
	require.NoError(t, auth.CacheFor(p, login).Write(t.Context()))
	return p
}

func testToken(t *testing.T, claims map[string]any) (token jwt.JWT) {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	require.NoError(t, token.UnmarshalText([]byte("e30."+base64.RawURLEncoding.EncodeToString(payload)+".test-signature")))
	return
}
