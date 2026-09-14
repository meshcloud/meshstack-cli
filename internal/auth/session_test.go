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
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testserver"
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

func TestSessionWithoutAnyCredentialSaysSo(t *testing.T) {
	newTestServer(t)

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	assert.ErrorContains(t, err, "selects no credential")
}

func TestSessionRefusesTwoCredentialsAtOnce(t *testing.T) {
	server := newTestServer(t)

	testApiKey1.SetEnv(t)
	t.Setenv(auth.ApiTokenSetting.EnvKey(), server.MintToken(t, time.Hour))

	_, err := auth.ResolveSession(t.Context(), testSessionOpts)
	assert.ErrorContains(t, err, "more than one credential")
}

func TestSessionPersistsARefreshedTokenForTheNextRun(t *testing.T) {
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

var (
	testSessionOpts = auth.ResolveSessionOptions{UserAgent: "test-client"}
	testApiKey1     = testserver.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-1"}
	testApiKey2     = testserver.ApiKey{ClientId: "22222222-45bf-42ba-a965-2097b9d0d181", ClientSecret: "super-test-secret-2"}
)

// newTestServer starts a backend both test api keys can log in to, and points a fresh config
// directory and the endpoint setting at it.
func newTestServer(t *testing.T) *testserver.Server {
	t.Helper()
	server := testserver.New(t, testApiKey1, testApiKey2)
	t.Setenv(profile.ConfigDirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), server.Url(t).String())
	return server
}

func greetingClient(session auth.Session) testserver.GreetingClient {
	return func(ctx context.Context, url *url.URL) (string, error) {
		return session.HttpClient.WithAuthorization(session).
			DoRequest[string](ctx, gohttp.MethodGet, url)
	}
}
