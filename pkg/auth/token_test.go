package auth

import (
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// apiKeyProfile is a profile whose current method is an API key, with the secret on disk so
// that nothing has to prompt.
func apiKeyProfile(t *testing.T, stack *fakeMeshStack, tokens map[workspace.Scope]profile.IssuedToken) {
	t.Helper()
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: stack.URL.String()}})
	writeCredentials(t, profile.Credentials{
		Endpoint:      stack.URL.String(),
		CurrentMethod: method.ApiKey,
		Methods:       profile.Methods{ApiKey: &profile.ApiKeyMethod{ClientId: "key-42", ClientSecret: testSecret}},
		AccessTokens:  tokens,
	})
}

// storedLogin is a login method that never gets as far as a request, for the rules that are
// decided before one is made.
func storedLogin() profile.Methods {
	return profile.Methods{Login: &profile.LoginMethod{RefreshToken: "refresh-old"}}
}

// loginProfile is a profile whose current method is the browser login. The endpoint is a
// parameter because the rules that need no request need no server either.
func loginProfile(t *testing.T, endpoint string, ws workspace.Name, methods profile.Methods) {
	t.Helper()
	writeConfig(t, "default", map[string]profile.Profile{
		"default": {Endpoint: endpoint, DefaultWorkspace: ws},
	})
	writeCredentials(t, profile.Credentials{
		Endpoint:      endpoint,
		CurrentMethod: method.Login,
		Methods:       methods,
	})
}

// TestHeaderMintsFromTheApiKeyOncePerTokenLife holds the two-cache rule: the header is
// produced before every request, and it does no I/O while the in-process token is good.
func TestHeaderMintsFromTheApiKeyOncePerTokenLife(t *testing.T) {
	stack := newMeshStack(t)
	at := isolate(t)
	t.Setenv(envEndpoint, stack.URL.String())
	t.Setenv(envApiKey, "key-42")
	t.Setenv(envApiSecret, testSecret)

	session := resolved(t, &fakeInput{secret: testSecret})

	first, err := session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Bearer api-key-token-1", first)
	require.Equal(t, 1, stack.apiLoginCount())

	second, err := session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, stack.apiLoginCount(), "a second call inside the token's life must make no request")

	requireNothingStored(t, at)
}

// TestATokenInsideTheGraceWindowIsRenewed holds the 30-second grace window: a token with
// less life than that left is renewed, and one with more is used as it is.
func TestATokenInsideTheGraceWindowIsRenewed(t *testing.T) {
	t.Run("inside the window", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, map[workspace.Scope]profile.IssuedToken{
			workspace.Unscoped: {Token: "nearly-expired", ExpiresAt: insideGrace()},
		})

		session := resolved(t, &fakeInput{})
		header, err := session.Header(t.Context())
		require.NoError(t, err)
		require.Equal(t, "Bearer api-key-token-1", header)
		require.Equal(t, 1, stack.apiLoginCount())
	})

	t.Run("outside the window", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, map[workspace.Scope]profile.IssuedToken{
			workspace.Unscoped: {Token: "still-good", ExpiresAt: futureExpiry()},
		})

		session := resolved(t, &fakeInput{})
		header, err := session.Header(t.Context())
		require.NoError(t, err)
		require.Equal(t, "Bearer still-good", header)
		require.Equal(t, 0, stack.apiLoginCount(), "another process's valid token must be used, not replaced")
	})
}

// TestTheLoginMethodRefreshesForTheWorkspaceAndStoresTheRotatedToken holds two rules at
// once: the refresh grant asks for c:<workspace>, and the rotated refresh token reaches the
// file in the same write as the access token it came with.
func TestTheLoginMethodRefreshesForTheWorkspaceAndStoresTheRotatedToken(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", profile.Methods{
		Login: &profile.LoginMethod{Issuer: stack.URL.String(), RefreshToken: "refresh-old"},
	})

	session := resolved(t, &fakeInput{})
	require.Equal(t, workspace.Name("demo"), session.Workspace)
	require.Equal(t, workspace.Scope("w:demo"), session.Scope())

	header, err := session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, stack.refreshCount())

	form := stack.lastRefreshForm(t)
	require.Equal(t, "openid c:demo", form.Get("scope"))
	require.Equal(t, "refresh_token", form.Get("grant_type"))
	require.Equal(t, "refresh-old", form.Get("refresh_token"))
	require.Equal(t, "meshstack-cli", form.Get("client_id"))

	stored := readCredentials(t)
	require.NotNil(t, stored.Methods.Login)
	require.Equal(t, "refresh-1", stored.Methods.Login.RefreshToken, "the rotated refresh token must replace the used one")
	require.Equal(t, header, "Bearer "+stored.AccessTokens[workspace.Scope("w:demo")].Token,
		"the access token and the refresh token it came with are one write")
}

// TestARefreshForAnotherWorkspaceFailsNamingTheWorkspace holds the MC_CUSTOMER check: a
// workspace the user is not a member of yields a token rather than an error, and the claim
// is what turns that into a message naming the workspace.
func TestARefreshForAnotherWorkspaceFailsNamingTheWorkspace(t *testing.T) {
	stack := newMeshStack(t)
	stack.answerRefreshWith(func(url.Values) (int, map[string]any) {
		return http.StatusOK, map[string]any{
			"access_token":  jwt(map[string]any{"MC_CUSTOMER": "somewhere-else"}),
			"refresh_token": "refresh-next",
			"expires_in":    300,
		}
	})
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", profile.Methods{
		Login: &profile.LoginMethod{Issuer: stack.URL.String(), RefreshToken: "refresh-old"},
	})

	session := resolved(t, &fakeInput{})
	_, err := session.Header(t.Context())
	p := problemOf(t, err)
	require.Equal(t, "this login cannot act in that workspace", p.Summary())
	assert.Contains(t, p.Detail(), "demo")
	assert.Contains(t, p.Detail(), "meshstack workspace list")
}

// TestRejectedForcesExactlyOneRemint holds the answer to a badly wrong clock: a 401 on a
// token this process believed valid re-mints once, and only once.
func TestRejectedForcesExactlyOneRemint(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	t.Setenv(envEndpoint, stack.URL.String())
	t.Setenv(envApiKey, "key-42")
	t.Setenv(envApiSecret, testSecret)

	session := resolved(t, &fakeInput{secret: testSecret})
	first, err := session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, stack.apiLoginCount())

	// A report about a header this session no longer holds is not a reason to re-mint.
	session.Rejected("Bearer a-token-from-another-session")
	_, err = session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, stack.apiLoginCount())

	session.Rejected(first)
	second, err := session.Header(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, 2, stack.apiLoginCount())

	// One re-mint, not a re-mint per request: the cached token is trusted again.
	_, err = session.Header(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, stack.apiLoginCount())
}

// TestADeadMethodNamesTheWayOut holds the failure table: renewal never switches method, so
// when the current one cannot mint, the command fails and says what to do about it.
func TestADeadMethodNamesTheWayOut(t *testing.T) {
	t.Run("manual names MESHSTACK_API_TOKEN", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: stack.URL.String()}})
		// A stored API token that has expired: nothing is left to mint from.
		writeCredentials(t, profile.Credentials{
			Endpoint:      stack.URL.String(),
			CurrentMethod: method.Manual,
		})

		session := resolved(t, &fakeInput{token: "unused"})
		_, err := session.Header(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "this API token has expired", p.Summary())
		assert.Contains(t, p.Detail(), envApiToken)
		assert.Contains(t, p.Detail(), "meshstack auth login --api-token")
	})

	t.Run("apiKey names the stored key id", func(t *testing.T) {
		stack := newMeshStack(t)
		// A token that is already inside the grace window is a method that cannot produce a
		// usable one, which is the outcome a deleted key or a changed secret ends in.
		stack.setApiLoginExpiry(0)
		isolate(t)
		apiKeyProfile(t, stack, nil)

		session := resolved(t, &fakeInput{})
		_, err := session.Header(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "this API key no longer works", p.Summary())
		assert.Contains(t, p.Detail(), "meshstack auth login --api-key=key-42")
	})

	t.Run("login names meshstack login", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		// A profile whose login method is gone: `auth logout` left the file, or the file was
		// written by a CLI that stored no refresh token.
		loginProfile(t, stack.URL.String(), "demo", profile.Methods{})

		session := resolved(t, &fakeInput{})
		_, err := session.Header(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "this login has expired or was revoked", p.Summary())
		assert.Contains(t, p.Detail(), "meshstack login")
		require.Equal(t, 0, stack.refreshCount())
	})
}

// TestRenewalNeverSwitchesMethod is the rule that keeps a command from quietly succeeding
// with a different identity than the user intended: a profile holding both methods fails on
// its current one rather than falling back to the other.
func TestRenewalNeverSwitchesMethod(t *testing.T) {
	stack := newMeshStack(t)
	stack.answerRefreshWith(func(url.Values) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{
			"error":             "invalid_grant",
			"error_description": "Session doesn't have required client",
		}
	})
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", profile.Methods{
		Login:  &profile.LoginMethod{Issuer: stack.URL.String(), RefreshToken: "refresh-old"},
		ApiKey: &profile.ApiKeyMethod{ClientId: "key-42", ClientSecret: testSecret},
	})

	session := resolved(t, &fakeInput{secret: testSecret})
	require.Equal(t, method.Login, session.Method())

	_, err := session.Header(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, oidc.ErrRefreshRejected)
	require.Equal(t, 0, stack.apiLoginCount(), "the API key in the same profile must not have been used")
	require.Equal(t, method.Login, session.Method())
}

// TestARejectedRefreshIsRetriedOnceWhenTheTokenWasAlreadyUsed holds the race-safety rule:
// "already used" almost always means another process rotated the token a moment ago, so the
// store is re-read and the grant retried once before the session is reported as ended.
func TestARejectedRefreshIsRetriedOnceWhenTheTokenWasAlreadyUsed(t *testing.T) {
	stack := newMeshStack(t)
	stack.answerRefreshWith(func(form url.Values) (int, map[string]any) {
		if stack.refreshCount() == 1 {
			return http.StatusBadRequest, map[string]any{
				"error":             "invalid_grant",
				"error_description": "Maximum allowed refresh token reuse exceeded",
			}
		}
		return refreshFromScope(form)
	})
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", profile.Methods{
		Login: &profile.LoginMethod{Issuer: stack.URL.String(), RefreshToken: "refresh-old"},
	})

	session := resolved(t, &fakeInput{})
	header, err := session.Header(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, header)
	require.Equal(t, 2, stack.refreshCount())
}

// TestTheStoredIssuerIsCheckedBeforeTheRefreshGrant holds the stricter of the two endpoint
// checks: it catches a repointed keycloak behind an unchanged meshStack endpoint.
func TestTheStoredIssuerIsCheckedBeforeTheRefreshGrant(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", profile.Methods{
		Login: &profile.LoginMethod{Issuer: "https://sso.somewhere-else.example.com", RefreshToken: "refresh-old"},
	})

	session := resolved(t, &fakeInput{})
	_, err := session.Header(t.Context())
	p := problemOf(t, err)
	require.Equal(t, "this login belongs to a different identity provider", p.Summary())
	assert.Contains(t, p.Detail(), "https://sso.somewhere-else.example.com")
	assert.Contains(t, p.Detail(), "meshstack login")
	require.Equal(t, 0, stack.refreshCount())
}

// TestRequireWorkspace holds the rule that a workspace is required for the browser login and
// optional for the credentials that carry their own.
func TestRequireWorkspace(t *testing.T) {
	t.Run("the login method needs one", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "", storedLogin())

		session := resolved(t, &fakeInput{})
		require.Empty(t, session.Workspace)
		p := problemOf(t, session.RequireWorkspace())
		require.Equal(t, "no workspace", p.Summary())
		assert.Contains(t, p.Detail(), "--workspace")
		assert.Contains(t, p.Detail(), "MESHSTACK_WORKSPACE")
		assert.Contains(t, p.Detail(), "meshstack profile set workspace")
	})

	t.Run("the login method with a workspace passes", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "demo", storedLogin())

		require.NoError(t, resolved(t, &fakeInput{}).RequireWorkspace())
	})

	t.Run("an API key carries its own", func(t *testing.T) {
		isolate(t)
		t.Setenv(envEndpoint, "https://api.example.com")
		t.Setenv(envApiKey, "key-42")
		t.Setenv(envApiSecret, testSecret)

		require.NoError(t, resolved(t, &fakeInput{secret: testSecret}).RequireWorkspace())
	})

	t.Run("an API token carries its own", func(t *testing.T) {
		isolate(t)
		t.Setenv(envEndpoint, "https://api.example.com")
		t.Setenv(envApiToken, "pasted-token")

		require.NoError(t, resolved(t, &fakeInput{token: "pasted-token"}).RequireWorkspace())
	})
}

// TestOnlyTheLoginMethodIsScopedToAWorkspace holds the token cache key: an API key or a
// pasted token carries whatever workspace its issuer put in it, and nothing re-scopes one.
func TestOnlyTheLoginMethodIsScopedToAWorkspace(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "demo", storedLogin())

		require.Equal(t, workspace.Scope("w:demo"), resolved(t, &fakeInput{}).Scope())
	})

	t.Run("apiKey", func(t *testing.T) {
		isolate(t)
		t.Setenv(envEndpoint, "https://api.example.com")
		t.Setenv(envApiKey, "key-42")
		t.Setenv(envApiSecret, testSecret)
		t.Setenv("MESHSTACK_WORKSPACE", "demo")

		session := resolved(t, &fakeInput{secret: testSecret})
		require.Equal(t, workspace.Name("demo"), session.Workspace, "the workspace still resolves; it is a request parameter too")
		require.Equal(t, workspace.Unscoped, session.Scope())
	})

	t.Run("manual", func(t *testing.T) {
		isolate(t)
		t.Setenv(envEndpoint, "https://api.example.com")
		t.Setenv(envApiToken, "pasted-token")
		t.Setenv("MESHSTACK_WORKSPACE", "demo")

		require.Equal(t, workspace.Unscoped, resolved(t, &fakeInput{token: "pasted-token"}).Scope())
	})
}

// TestConcurrentHeaderCallsMintOnce holds the rule a Terraform provider depends on: many
// requests in flight at once cost one token, not one each.
func TestConcurrentHeaderCallsMintOnce(t *testing.T) {
	run := func(t *testing.T, callers int, stack *fakeMeshStack, session *Session) {
		t.Helper()
		headers := make([]string, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				header, err := session.Header(t.Context())
				assert.NoError(t, err)
				headers[i] = header
			}()
		}
		wg.Wait()

		require.Equal(t, 1, stack.apiLoginCount(), "every goroutine but one must have used the token the first minted")
		for _, header := range headers {
			require.Equal(t, "Bearer api-key-token-1", header)
		}
	}

	t.Run("a memory store", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		t.Setenv(envEndpoint, stack.URL.String())
		t.Setenv(envApiKey, "key-42")
		t.Setenv(envApiSecret, testSecret)

		run(t, 32, stack, resolved(t, &fakeInput{secret: testSecret}))
	})

	// The lock is the riskier half: the waiters have to re-read under it and find the
	// winner's token rather than mint one each.
	t.Run("a file store under its lock", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, nil)

		run(t, 8, stack, resolved(t, &fakeInput{}))
	})
}
