package auth_test

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
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

func TestStatusOfABrowserLoginIsReadFromTheCacheAlone(t *testing.T) {
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

	session, err := auth.StoredSession(t.Context(), p, testSessionOpts)
	require.NoError(t, err)
	status := session.Status(t.Context())

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
}

func TestStatusListsTheStoredCredentialsBesidesTheOneInUse(t *testing.T) {
	newTestServer(t)
	p := storedOidcLogin(t, xurl.MustParsef("https://localhost:1/realms/meshfed"), "", oidc.Token{
		RefreshToken: "refresh-token",
		AccessToken:  testToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
	})
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(&credential.ApiKey{Endpoint: p.Endpoint, ClientId: uuid.MustParse(testApiKey2.ClientId), ClientSecret: testApiKey2.ClientSecret})
	require.NoError(t, creds.Store(t.Context()))

	session, err := auth.StoredSession(t.Context(), p, testSessionOpts)
	require.NoError(t, err)

	assert.Equal(t, []auth.UnusedCredential{{Kind: credential.ApiKeyName, ClientId: testApiKey2.ClientId}}, session.Status(t.Context()).Unused)
}

func TestStatusOfAnApiKeyReadsTheKeyFromMeshStack(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		want     *auth.ApiKeyDetails
		wantWarn string
	}{
		{
			name:   "a key reads itself",
			status: gohttp.StatusOK,
			body: `{"metadata":{"uuid":"` + testApiKey1.ClientId + `","ownedByWorkspace":"ops"},` +
				`"spec":{"displayName":"CI","permissions":["WORKSPACE_LIST","APIKEY_LIST"],"expiresAt":"2027-01-31"},` +
				`"status":{"clientId":"` + testApiKey1.ClientId + `","expiresAt":"2027-02-01T00:00:00Z"}}`,
			want: &auth.ApiKeyDetails{
				DisplayName: "CI", OwnedByWorkspace: "ops", ExpiresAt: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC),
				Permissions: []string{"APIKEY_LIST", "WORKSPACE_LIST"},
				PermissionGroups: client.ApiKeyPermissions{
					{Name: "API Keys", Actions: [][]client.ApiPermission{{"APIKEY_LIST"}}},
					{Name: "Workspaces", Actions: [][]client.ApiPermission{{"WORKSPACE_LIST"}}},
				},
			},
		},
		{
			name:   "a meshStack that sends no expiry time leaves the expiry date",
			status: gohttp.StatusOK,
			body: `{"metadata":{"uuid":"` + testApiKey1.ClientId + `","ownedByWorkspace":"ops"},` +
				`"spec":{"displayName":"CI","permissions":[],"expiresAt":"2027-01-31"},"status":{"clientId":"` + testApiKey1.ClientId + `"}}`,
			want: &auth.ApiKeyDetails{DisplayName: "CI", OwnedByWorkspace: "ops", ExpiresOn: "2027-01-31", Permissions: []string{}},
		},
		{name: "a meshStack without /self forbids it to a key without APIKEY_LIST", status: gohttp.StatusForbidden, wantWarn: "does not let an API key read itself"},
		{name: "a meshStack without /self finds no key of that uuid", status: gohttp.StatusNotFound, wantWarn: "does not let an API key read itself"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t)
			// The test server answers neither /mesh/info nor GitHub, as for auth status --skip-version-check.
			t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")
			server.Route(t, "/api/meshobjects/meshapikeys/self", func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
				resp.WriteHeader(tt.status)
				_, _ = fmt.Fprint(resp, tt.body)
			})
			testApiKey1.SetEnv(t)
			captured := logs.Capture(t)

			session, err := auth.ResolveSession(t.Context(), testSessionOpts)
			require.NoError(t, err)
			status := session.Status(t.Context())

			assert.Equal(t, []string{"env MESHSTACK_API_KEY", "env MESHSTACK_API_SECRET"}, status.Sources)
			require.NotNil(t, status.ApiKey)
			assert.Equal(t, testApiKey1.ClientId, status.ApiKey.ClientId.String())
			assert.Equal(t, tt.want, status.ApiKey.Details)
			if tt.wantWarn != "" {
				assert.Contains(t, captured.String(), "level=WARN")
				assert.Contains(t, captured.String(), tt.wantWarn)
			} else {
				assert.Empty(t, captured.Records(slog.LevelWarn))
			}
			assert.NotNil(t, status.Token, "reading the key minted a token")
		})
	}
}

func TestStatusOfAnApiTokenOfABuildingBlockRunReadsItsEphemeralKey(t *testing.T) {
	server := newTestServer(t)
	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")
	server.Route(t, "/api/meshobjects/meshapikeys/self", func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = fmt.Fprint(resp, `{"metadata":{"uuid":"`+testApiKey1.ClientId+`","ownedByWorkspace":"ops"},`+
			`"spec":{"displayName":"run 42","permissions":["BUILDINGBLOCK_SAVE"]}}`)
	})
	token := testToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "client_id": testApiKey1.ClientId})
	t.Setenv(auth.ApiTokenSetting.EnvKey(), token.String())

	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	status := session.Status(t.Context())

	assert.Equal(t, credential.ManualName, status.Kind)
	require.NotNil(t, status.ApiKey)
	assert.Equal(t, testApiKey1.ClientId, status.ApiKey.ClientId.String())
	require.NotNil(t, status.ApiKey.Details)
	assert.Equal(t, "run 42", status.ApiKey.Details.DisplayName)
	assert.Equal(t, []string{"BUILDINGBLOCK_SAVE"}, status.ApiKey.Details.Permissions)
}

func TestStatusOfAnExpiredApiTokenSaysSo(t *testing.T) {
	newTestServer(t)
	token := testToken(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix(), "client_id": testApiKey1.ClientId})
	t.Setenv(auth.ApiTokenSetting.EnvKey(), token.String())
	captured := logs.Capture(t)

	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	status := session.Status(t.Context())

	assert.Equal(t, credential.ManualName, status.Kind)
	assert.Equal(t, []string{"env MESHSTACK_API_TOKEN"}, status.Sources)
	require.NotNil(t, status.Token)
	assert.True(t, status.Token.Expired())
	assert.Equal(t, testApiKey1.ClientId, status.Token.ClientId)
	require.NotNil(t, status.ApiKey)
	assert.Nil(t, status.ApiKey.Details, "an expired token cannot read its key")
	assert.Empty(t, captured.Records(slog.LevelWarn))
}

func TestStatusShowsTheApiTokenFromTheEnvironmentAndTheStoredOneAsUnused(t *testing.T) {
	newTestServer(t)
	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")
	t.Setenv(auth.ApiTokenSetting.EnvKey(), testToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "preferred_username": "stored"}).String())
	_, store, unlock, err := auth.Login(t.Context(), credential.ManualName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())

	t.Setenv(auth.ApiTokenSetting.EnvKey(), testToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "preferred_username": "env"}).String())
	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	status := session.Status(t.Context())

	assert.Equal(t, []string{"env MESHSTACK_API_TOKEN"}, status.Sources)
	require.NotNil(t, status.Token)
	assert.Equal(t, "env", status.Token.User)
	require.Len(t, status.Unused, 1)
	assert.True(t, status.Unused[0].Selected)
	require.NotNil(t, status.Unused[0].Token)
	assert.Equal(t, "stored", status.Unused[0].Token.User)
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
