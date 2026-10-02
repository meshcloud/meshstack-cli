package auth_test

import (
	"context"
	"fmt"
	gohttp "net/http"
	gohttptest "net/http/httptest"
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
	"github.com/meshcloud/meshstack-cli/internal/testutil/testserver"
)

func TestSessionAuthorizesWithAnApiKey(t *testing.T) {
	server := newTestServer(t)

	testApiKey1.SetEnv(t)
	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	greet := greetingClient(session)

	server.RequireGreeting(t, greet)
	assert.EqualValues(t, 1, server.Counts(t).Logins, "one login mints the token every request then reuses")

	server.RequireGreeting(t, greet)
	assert.EqualValues(t, 1, server.Counts(t).Logins, "the cached token is still valid, so nothing is re-minted")
}

func TestSessionRefreshesARejectedToken(t *testing.T) {
	server := newTestServer(t)

	testApiKey1.SetEnv(t)
	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	greet := greetingClient(session)

	server.RequireGreeting(t, greet)
	require.EqualValues(t, 1, server.Counts(t).Logins)

	require.True(t, server.RevokeNewestToken(t), "the token the session just minted")
	server.RequireGreeting(t, greet)

	assert.EqualValues(t, 2, server.Counts(t).Logins, "a 401 mints once more, on demand")
}

func TestSessionOnAFreshConfigDirectoryWithoutEndpointNamesTheSetting(t *testing.T) {
	newTestServer(t)
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "")

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.ErrorContains(t, err, meshstack.EndpointSetting.EnvKey())
}

func TestSessionWithoutAnyCredentialNamesTheSettingsItLookedFor(t *testing.T) {
	newTestServer(t)

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.ErrorContains(t, err, "selects no credential")
	require.ErrorContains(t, err, auth.ApiTokenSetting.EnvKey())
	require.ErrorContains(t, err, auth.ApiKeyClientIdSetting.EnvKey())
	require.ErrorContains(t, err, auth.ApiKeyClientSecretSetting.EnvKey())
}

func TestSessionWithHalfAnApiKeySaysWhichHalfIsMissing(t *testing.T) {
	newTestServer(t)

	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), testApiKey1.ClientId)

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.ErrorContains(t, err, "together")
	require.ErrorContains(t, err, auth.ApiKeyClientSecretSetting.EnvKey())
}

func TestSessionRefusesTwoCredentialsAtOnce(t *testing.T) {
	server := newTestServer(t)

	testApiKey1.SetEnv(t)
	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	assert.ErrorContains(t, err, "more than one credential")
}

func TestSessionReusesAStoredTokenUntilTheApiKeyChanges(t *testing.T) {
	// The config directory newTestServer sets is the parent's, so every subtest below shares
	// it while each one decides its own credential environment.
	server := newTestServer(t)

	t.Run("login", func(t *testing.T) {
		testApiKey1.SetEnv(t)

		login, store, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(login))
		require.NoError(t, store(t.Context()))
		require.NoError(t, unlock())
	})

	t.Run("reloaded without env", func(t *testing.T) {
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		assert.EqualValues(t, 1, server.Counts(t).Logins)
	})

	t.Run("reloaded with same env", func(t *testing.T) {
		testApiKey1.SetEnv(t)
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		assert.EqualValues(t, 1, server.Counts(t).Logins)
	})

	t.Run("reloaded with different env, minting its own token", func(t *testing.T) {
		testApiKey2.SetEnv(t)
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		assert.EqualValues(t, 2, server.Counts(t).Logins)
	})

	t.Run("reloaded without env again, finding the stored api key's cache untouched", func(t *testing.T) {
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		assert.EqualValues(t, 2, server.Counts(t).Logins)
	})
}

func TestSessionStoresTheFirstTokenMintedAfterALoginThatMintedNone(t *testing.T) {
	server := newTestServer(t)
	testApiKey1.SetEnv(t)
	_, store, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())
	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), "")
	t.Setenv(auth.ApiKeyClientSecretSetting.EnvKey(), "")

	for range 2 {
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
	}

	assert.EqualValues(t, 1, server.Counts(t).Logins)
}

func TestSessionSendsTheManualTokenFromTheEnvironmentOverTheStoredOne(t *testing.T) {
	server := newTestServer(t)
	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))
	_, store, unlock, err := auth.Login(t.Context(), credential.ManualName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())
	require.True(t, server.RevokeNewestToken(t), "the token the login stored")

	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))
	session, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)

	server.RequireGreeting(t, greetingClient(session))
}

func TestSessionKeepsTheStoredManualTokenApartFromARejectedOneFromTheEnvironment(t *testing.T) {
	server := newTestServer(t)
	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))
	_, store, unlock, err := auth.Login(t.Context(), credential.ManualName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())

	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))
	require.True(t, server.RevokeNewestToken(t), "the token in the environment")
	fromEnvironment, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	require.Error(t, server.Greeting(t, t.Context(), greetingClient(fromEnvironment)), "the stored token does not stand in for a rejected one")

	t.Setenv(auth.ApiTokenSetting.EnvKey(), "")
	stored, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	server.RequireGreeting(t, greetingClient(stored))
}

func TestLoginCreatesAndStoresTheProfileItNames(t *testing.T) {
	newTestServer(t)
	testApiKey1.SetEnv(t)
	// The first login writes profiles.json, so the name below is one missing from a file that
	// does exist.
	_, storeFirst, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, storeFirst(t.Context()))
	require.NoError(t, unlock())

	t.Setenv(profile.NameSetting.EnvKey(), "dev")
	_, store, unlock, err := auth.Login(t.Context(), credential.ApiKeyName, testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, store(t.Context()))
	require.NoError(t, unlock())

	_, err = auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err, "the created profile is on disk for every later command")

	t.Setenv(profile.NameSetting.EnvKey(), "")
	current, _, err := profile.ResolveProfile(t.Context(), profile.ResolveProfileOptions{})
	require.NoError(t, err)
	assert.Equal(t, profile.Name("dev"), current.Name, "the login selected the profile it created")
}

var (
	testSessionOpts = auth.ResolveSessionOptions{Version: "dev", GitHubRepo: "meshcloud/test-client"}
	testApiKey1     = testserver.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-1"}
	testApiKey2     = testserver.ApiKey{ClientId: "22222222-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-2"}
)

func newTestServer(t *testing.T) *testserver.Server {
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
	server := testserver.New(t, testApiKey1, testApiKey2)
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), server.Url(t).String())
	return server
}

// greetingClient brings its own http.Client, because the session keeps its own to itself. What
// these tests drive is the authorization, which the session supplies either way.
func greetingClient(session auth.Session) testserver.GreetingClient {
	return func(ctx context.Context, url *url.URL) (string, error) {
		return http.NewClient("session-test").WithAuthorization(session).
			DoRequest[string](ctx, gohttp.MethodGet, url)
	}
}

func TestARefreshFinishesAndIsCachedThoughItsContextIsCancelledMidway(t *testing.T) {
	newTestServer(t)
	requested, release := make(chan struct{}), make(chan struct{})
	slowLogin := gohttptest.NewServer(gohttp.HandlerFunc(func(resp gohttp.ResponseWriter, req *gohttp.Request) {
		if req.URL.Path != "/api/login" {
			resp.WriteHeader(gohttp.StatusNotFound)
			return
		}
		close(requested)
		<-release
		_, _ = fmt.Fprintf(resp, `{"access_token":%q}`, testToken(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()}).String())
	}))
	t.Cleanup(slowLogin.Close)
	p := &profile.Profile{
		Name: "slow", Endpoint: xurl.URL{URL: must(url.Parse(slowLogin.URL))}, Credential: credential.ApiKeyName,
		ConfigDir: config.Directory(t.TempDir()),
	}
	apiKey := &credential.ApiKey{Endpoint: p.Endpoint, ClientId: uuid.MustParse(testApiKey1.ClientId), ClientSecret: testApiKey1.ClientSecret}
	creds, err := p.Credentials(t.Context())
	require.NoError(t, err)
	creds.Set(apiKey)
	require.NoError(t, creds.Store(t.Context()))
	require.NoError(t, auth.CacheFor(p, apiKey).Write(t.Context()))

	// The version checks would call the login server, which knows only the login.
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
