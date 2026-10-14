package auth_test

import (
	"context"
	"fmt"
	gohttp "net/http"
	"net/url"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestSessionWithAnApiKeyFromTheEnvironment(t *testing.T) {
	server := newTestServer(t)
	setApiKeyEnv(t, testApiKey1)
	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)

	t.Run("two requests log in only once", func(t *testing.T) {
		requireAnswer(t, server, session)
		requireAnswer(t, server, session)
		assert.EqualValues(t, 1, server.Counts().Logins)
	})

	t.Run("a request that gets a 401 logs in once more", func(t *testing.T) {
		require.True(t, server.RevokeNewestToken(), "the token the session minted")
		requireAnswer(t, server, session)
		assert.EqualValues(t, 2, server.Counts().Logins)
	})
}

func TestSessionResolutionSaysWhyItFails(t *testing.T) {
	server := newTestServer(t)

	t.Run("without an endpoint", func(t *testing.T) {
		t.Setenv(meshstack.EndpointSetting.EnvKey(), "")
		_, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.ErrorContains(t, err, meshstack.EndpointSetting.EnvKey())
	})

	t.Run("without any credential", func(t *testing.T) {
		_, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.ErrorContains(t, err, "selects no credential")
		require.ErrorContains(t, err, auth.ApiTokenSetting.EnvKey())
		require.ErrorContains(t, err, auth.ApiKeyClientIdSetting.EnvKey())
		require.ErrorContains(t, err, auth.ApiKeyClientSecretSetting.EnvKey())
	})

	t.Run("with half an API key", func(t *testing.T) {
		t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), testApiKey1.ClientId)
		_, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.ErrorContains(t, err, "together")
		require.ErrorContains(t, err, auth.ApiKeyClientSecretSetting.EnvKey())
	})

	t.Run("with two credentials at once", func(t *testing.T) {
		setApiKeyEnv(t, testApiKey1)
		t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(time.Hour))
		_, err := auth.ResolveSession(t.Context(), testSessionOpts)
		assert.ErrorContains(t, err, "more than one credential")
	})
}

func TestSessionOfAStoredApiKey(t *testing.T) {
	server := newTestServer(t)
	askWithResolvedSession := func(t *testing.T) {
		t.Helper()
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		requireAnswer(t, server, session)
	}

	t.Run("a login stores the token it minted", func(t *testing.T) {
		setApiKeyEnv(t, testApiKey1)
		requireAnswer(t, server, storeLogin(t, credential.ApiKeyName))
		assert.EqualValues(t, 1, server.Counts().Logins)
	})

	t.Run("the next command reuses that token", func(t *testing.T) {
		askWithResolvedSession(t)
		assert.EqualValues(t, 1, server.Counts().Logins)
	})

	t.Run("the same API key in the environment reuses it too", func(t *testing.T) {
		setApiKeyEnv(t, testApiKey1)
		askWithResolvedSession(t)
		assert.EqualValues(t, 1, server.Counts().Logins)
	})

	t.Run("another API key in the environment mints its own token", func(t *testing.T) {
		setApiKeyEnv(t, testApiKey2)
		askWithResolvedSession(t)
		assert.EqualValues(t, 2, server.Counts().Logins)
	})

	t.Run("without the environment again, the stored API key reuses its own token", func(t *testing.T) {
		askWithResolvedSession(t)
		assert.EqualValues(t, 2, server.Counts().Logins)
	})

	t.Run("a login to the profile it names creates and selects that profile", func(t *testing.T) {
		setApiKeyEnv(t, testApiKey1)
		t.Setenv(profile.NameSetting.EnvKey(), "dev")
		storeLogin(t, credential.ApiKeyName)

		t.Setenv(profile.NameSetting.EnvKey(), "")
		current, _, err := profile.ResolveProfile(t.Context(), profile.ResolveProfileOptions{})
		require.NoError(t, err)
		assert.Equal(t, profile.Name("dev"), current.Name)
	})

	t.Run("the first token minted after a login that minted none is stored", func(t *testing.T) {
		askWithResolvedSession(t)
		askWithResolvedSession(t)
		assert.EqualValues(t, 3, server.Counts().Logins)
	})
}

func TestSessionOfAStoredManualToken(t *testing.T) {
	server := newTestServer(t)
	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(time.Hour))
	storeLogin(t, credential.ManualName)

	t.Run("the stored token does not stand in for a rejected one from the environment", func(t *testing.T) {
		t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(time.Hour))
		require.True(t, server.RevokeNewestToken(), "the token in the environment")
		fromEnvironment, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		require.Error(t, ask(t.Context(), server, fromEnvironment))

		t.Setenv(auth.ApiTokenSetting.EnvKey(), "")
		stored, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		requireAnswer(t, server, stored)
	})

	t.Run("a token from the environment is sent over the stored one", func(t *testing.T) {
		require.True(t, server.RevokeNewestToken(), "the token the login stored")
		t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(time.Hour))
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		requireAnswer(t, server, session)
	})
}

func storeLogin(t *testing.T, credentialName credential.Name) auth.Session {
	t.Helper()
	session, store, unlock, err := auth.Login(t.Context(), credentialName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())
	return session
}

var (
	testSessionOpts = auth.ResolveSessionOptions{Version: "dev", GitHubRepo: "meshcloud/test-client"}
	testApiKey1     = fakemeshstack.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-1"}
	testApiKey2     = fakemeshstack.ApiKey{ClientId: "22222222-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-2"}
)

func newTestServer(t *testing.T) *fakemeshstack.Server {
	t.Helper()
	// A shell that exports any of these would otherwise reach the resolutions under test, and a
	// MESHSTACK_API_TOKEN of its own resolves a credential no test here asked for. An empty value
	// is skipped as no value at all, see Setting.Resolve.
	for _, envKey := range []string{
		profile.NameSetting.EnvKey(),
		meshstack.WorkspaceSetting.EnvKey(),
		meshstack.SkipVersionCheckSetting.EnvKey(),
		auth.ApiKeyClientIdSetting.EnvKey(),
		auth.ApiKeyClientSecretSetting.EnvKey(),
		auth.ApiTokenSetting.EnvKey(),
	} {
		t.Setenv(envKey, "")
	}
	server := fakemeshstack.Start(t, fakemeshstack.Options{ApiKeys: []fakemeshstack.ApiKey{testApiKey1, testApiKey2}})
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), server.URL)
	return server
}

func setApiKeyEnv(t *testing.T, key fakemeshstack.ApiKey) {
	t.Helper()
	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), key.ClientId)
	t.Setenv(auth.ApiKeyClientSecretSetting.EnvKey(), key.ClientSecret)
}

// ask brings its own http.Client, because the session keeps its own to itself. What these tests
// drive is the authorization, which the session supplies either way.
func ask(ctx context.Context, server *fakemeshstack.Server, session auth.Session) error {
	_, err := http.NewClient(fakemeshstack.UserAgent).WithAuthorization(session).
		DoRequest[[]byte](ctx, gohttp.MethodGet, must(url.Parse(server.URL+"/api/meshobjects/meshworkspaces")))
	return err
}

func requireAnswer(t *testing.T, server *fakemeshstack.Server, session auth.Session) {
	t.Helper()
	require.NoError(t, ask(t.Context(), server, session))
}

func TestARefreshFinishesAndIsCachedThoughItsContextIsCancelledMidway(t *testing.T) {
	server := newTestServer(t)
	requested, release := make(chan struct{}), make(chan struct{})
	server.Route("POST /api/login", func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
		close(requested)
		<-release
		_, _ = fmt.Fprintf(resp, `{"access_token":%q}`, server.MintToken(time.Hour))
	})
	p := &profile.Profile{
		Name: "slow", Endpoint: xurl.MustParsef("%s", server.URL), Credential: credential.ApiKeyName,
		ConfigDir: config.Directory(t.TempDir()),
	}
	apiKey := &credential.ApiKey{Endpoint: p.Endpoint, ClientId: uuid.MustParse(testApiKey1.ClientId), ClientSecret: testApiKey1.ClientSecret}
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(apiKey)
	require.NoError(t, creds.Store(t.Context()))
	require.NoError(t, auth.CacheFor(p, apiKey).Write(t.Context()))

	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")
	ctx, cancel := context.WithCancel(t.Context())
	session, err := auth.StoredSession(ctx, p, testSessionOpts)
	require.NoError(t, err)
	c, err := session.Client()
	require.NoError(t, err)
	listed := make(chan error, 1)
	go func() {
		_, err := c.Workspace.List(ctx)
		listed <- err
	}()
	<-requested
	cancel()
	close(release)

	require.ErrorIs(t, <-listed, context.Canceled)
	reloaded := &credential.ApiKey{Endpoint: p.Endpoint, ClientId: apiKey.ClientId, ClientSecret: apiKey.ClientSecret}
	require.NoError(t, auth.CacheFor(p, reloaded).Load(t.Context()))
	assert.NotNil(t, reloaded.Cache, "the refresh finished and cached its token before the call returned")
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
