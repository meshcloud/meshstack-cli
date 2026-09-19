package auth_test

import (
	"context"
	gohttp "net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
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

func TestSessionWithoutAnyCredentialNamesTheSettingsItLookedFor(t *testing.T) {
	newTestServer(t)

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.ErrorContains(t, err, "selects none")
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

	t.Run("first run", func(t *testing.T) {
		testApiKey1.SetEnv(t)

		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		require.NoError(t, session.Store(t.Context()))
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

	t.Run("reloaded with different env, busting the cache", func(t *testing.T) {
		testApiKey2.SetEnv(t)
		session, err := auth.ResolveSession(t.Context(), testSessionOpts)
		require.NoError(t, err)
		server.RequireGreeting(t, greetingClient(session))
		assert.EqualValues(t, 2, server.Counts(t).Logins)
	})
}

func TestSessionOfALoginCreatesAndStoresTheProfileItNames(t *testing.T) {
	newTestServer(t)
	testApiKey1.SetEnv(t)
	// The first session writes profiles.json, so the name below is one missing from a file that
	// does exist, which is the case every command but a login rejects.
	firstSession, err := auth.ResolveSession(t.Context(), testSessionOpts)
	require.NoError(t, err)
	require.NoError(t, firstSession.Store(t.Context()))

	t.Setenv(profile.NameSetting.EnvKey(), "dev")
	_, err = auth.ResolveSession(t.Context(), testSessionOpts)
	require.ErrorContains(t, err, "no profile found with name dev")

	loginOpts := testSessionOpts
	loginOpts.CreateProfileIfMissing = true
	login, err := auth.ResolveSession(t.Context(), loginOpts)
	require.NoError(t, err)
	require.NoError(t, login.Store(t.Context()))

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

// newTestServer starts a backend both test api keys can log in to, and points a fresh config
// directory and the endpoint setting at it.
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
