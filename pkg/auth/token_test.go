package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

// apiKeyProfile is a profile whose current method is an API key, with the secret on disk so
// that nothing has to prompt.
func apiKeyProfile(t *testing.T, stack *fakeMeshStack, token credential.IssuedToken) {
	t.Helper()
	writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl(stack.URL.String())}})
	writeCredentials(t, profile.Credentials{
		Endpoint: mustUrl(stack.URL.String()),
		Credential: credential.FromApiKey(credential.ApiKey{
			Id: "key-42", Secret: testSecret, AccessToken: token,
		}),
	})
}

// storedLogin is a login method that never gets as far as a request, for the rules that are
// decided before one is made.
func storedLogin() credential.Credential {
	return credential.FromLogin(credential.Login{RefreshToken: "refresh-old"})
}

// loginProfile is a profile whose current method is the browser login. The endpoint is a
// parameter because the rules that need no request need no server either.
func loginProfile(t *testing.T, endpoint string, ws string, held credential.Credential) {
	t.Helper()
	held.Current = credential.MethodLogin
	writeConfig(t, "default", map[string]profile.Profile{
		"default": {Endpoint: mustUrl(endpoint), DefaultWorkspace: ws},
	})
	writeCredentials(t, profile.Credentials{Endpoint: mustUrl(endpoint), Credential: held})
}

// TestBearerTokenMintsFromTheApiKeyOncePerTokenLife holds the two-cache rule: the token is
// produced before every request, and it does no I/O while the in-process token is good.
func TestBearerTokenMintsFromTheApiKeyOncePerTokenLife(t *testing.T) {
	stack := newMeshStack(t)
	at := isolate(t)
	t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
	t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{})

	first, err := session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, "api-key-token-1", tokenId(first))
	require.Equal(t, 1, stack.apiLoginCount())

	second, err := session.BearerToken(t.Context())
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
		apiKeyProfile(t, stack, credential.IssuedToken{Token: mustJwt(fakeToken("nearly-expired")), ExpiresAt: insideGrace()})

		session := resolved(t, ResolveSessionOptions{})
		token, err := session.BearerToken(t.Context())
		require.NoError(t, err)
		require.Equal(t, "api-key-token-1", tokenId(token))
		require.Equal(t, 1, stack.apiLoginCount())
	})

	t.Run("outside the window", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, credential.IssuedToken{Token: mustJwt(fakeToken("still-good")), ExpiresAt: futureExpiry()})

		session := resolved(t, ResolveSessionOptions{})
		token, err := session.BearerToken(t.Context())
		require.NoError(t, err)
		require.Equal(t, fakeToken("still-good"), token)
		require.Equal(t, 0, stack.apiLoginCount(), "another process's valid token must be used, not replaced")
	})
}

// TestTheLoginMethodRefreshesForTheWorkspaceAndStoresTheRotatedToken holds two rules at
// once: the refresh grant asks for c:<workspace>, and the rotated refresh token reaches the
// file in the same write as the access token it came with.
func TestTheLoginMethodRefreshesForTheWorkspaceAndStoresTheRotatedToken(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
		Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old",
	}))

	session := resolved(t, ResolveSessionOptions{})
	require.Equal(t, "demo", session.Workspace)
	require.Equal(t, meshstack.WorkspaceScope("demo"), session.Scope())

	token, err := session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, stack.refreshCount())

	form := stack.lastRefreshForm(t)
	require.Equal(t, "openid c:demo", form.Get("scope"))
	require.Equal(t, "refresh_token", form.Get("grant_type"))
	require.Equal(t, "refresh-old", form.Get("refresh_token"))
	require.Equal(t, "meshstack-cli", form.Get("client_id"))

	stored := readCredentials(t)
	require.NotNil(t, stored.Login)
	require.Equal(t, "refresh-1", stored.Login.RefreshToken, "the rotated refresh token must replace the used one")
	require.Equal(t, token, stored.Login.AccessTokens[meshstack.WorkspaceScope("demo")].Token.String,
		"the access token and the refresh token it came with are one write")
}

// TestARefreshForAnotherWorkspaceFailsNamingTheWorkspace holds the MC_CUSTOMER check: a
// workspace the user is not a member of yields a token rather than an error, and the claim
// is what turns that into a message naming the workspace.
func TestARefreshForAnotherWorkspaceFailsNamingTheWorkspace(t *testing.T) {
	stack := newMeshStack(t)
	stack.answerRefreshWith(func(url.Values) (int, map[string]any) {
		return http.StatusOK, map[string]any{
			"access_token":  fakeJwt(map[string]any{"MC_CUSTOMER": "somewhere-else"}),
			"refresh_token": "refresh-next",
			"expires_in":    300,
		}
	})
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
		Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old",
	}))

	session := resolved(t, ResolveSessionOptions{})
	_, err := session.BearerToken(t.Context())
	p := problemOf(t, err)
	require.Equal(t, "this login cannot act in that workspace", p.Summary())
	assert.Contains(t, p.Detail(), "demo")
	assert.Contains(t, p.Detail(), "meshstack workspace list")
}

// TestRefreshBearerTokenForcesExactlyOneRemint holds the answer to a badly wrong clock: a 401
// on a token this process believed valid re-mints once, and only once.
func TestRefreshBearerTokenForcesExactlyOneRemint(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
	t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
	t.Setenv(credential.ApiSecret.EnvKey, testSecret)

	session := resolved(t, ResolveSessionOptions{})
	first, err := session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, stack.apiLoginCount())

	// A 401 on a token this session no longer holds belongs to a request that was in flight
	// while another one replaced it. The replacement is handed back, and nothing is minted.
	replacement, err := session.RefreshBearerToken(t.Context(), "a-token-from-another-session")
	require.NoError(t, err)
	require.Equal(t, first, replacement)
	require.Equal(t, 1, stack.apiLoginCount())

	second, err := session.RefreshBearerToken(t.Context(), first)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, 2, stack.apiLoginCount())

	// One re-mint, not a re-mint per request: the cached token is trusted again.
	_, err = session.BearerToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, stack.apiLoginCount())
}

// TestTheApiKeyExchangeSeparatesARefusalFromAnOutage holds the two answers /api/login can give
// that are not a token. A 401 means the secret is wrong and says so; anything else is reported
// as a failed login. The 401 must also not be replayed, which is the transport's rule and the
// reason the exchange may ask to be retried at all.
func TestTheApiKeyExchangeSeparatesARefusalFromAnOutage(t *testing.T) {
	t.Run("a 401 names the key and meshPanel", func(t *testing.T) {
		stack := newMeshStack(t)
		stack.answerApiLoginWith(http.StatusUnauthorized)
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		session := resolved(t, ResolveSessionOptions{})
		_, err := session.BearerToken(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "meshStack refused this API key", p.Summary())
		assert.Contains(t, p.Detail(), "key-42")
		assert.Contains(t, p.Detail(), "meshPanel")
		require.Equal(t, 1, stack.apiLoginCount(), "a wrong secret must not be retried")
	})

	t.Run("any other refusal is reported as a failed login", func(t *testing.T) {
		stack := newMeshStack(t)
		stack.answerApiLoginWith(http.StatusForbidden)
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		session := resolved(t, ResolveSessionOptions{})
		_, err := session.BearerToken(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "could not log in to meshStack with an API key", p.Summary())
		assert.Contains(t, p.Detail(), "key-42")
	})
}

// TestADeadMethodNamesTheWayOut holds the failure table: renewal never switches method, so
// when the current one cannot mint, the command fails and says what to do about it.
func TestADeadMethodNamesTheWayOut(t *testing.T) {
	t.Run("manual names MESHSTACK_API_TOKEN", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		writeConfig(t, "default", map[string]profile.Profile{"default": {Endpoint: mustUrl(stack.URL.String())}})
		// A stored API token that has expired: nothing is left to mint from.
		writeCredentials(t, profile.Credentials{
			Endpoint:   mustUrl(stack.URL.String()),
			Credential: credential.FromManual(credential.Manual{}),
		})

		session := resolved(t, ResolveSessionOptions{})
		_, err := session.BearerToken(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "this API token has expired", p.Summary())
		assert.Contains(t, p.Detail(), credential.ApiToken.EnvKey)
		assert.Contains(t, p.Detail(), "meshstack auth login --api-token")
	})

	t.Run("apiKey names the stored key id", func(t *testing.T) {
		stack := newMeshStack(t)
		// A token that is already inside the grace window is a method that cannot produce a
		// usable one, which is the outcome a deleted key or a changed secret ends in.
		stack.setApiLoginExpiry(0)
		isolate(t)
		apiKeyProfile(t, stack, credential.IssuedToken{})

		session := resolved(t, ResolveSessionOptions{})
		_, err := session.BearerToken(t.Context())
		p := problemOf(t, err)
		require.Equal(t, "this API key no longer works", p.Summary())
		assert.Contains(t, p.Detail(), "meshstack auth login --api-key=key-42")
	})

	t.Run("login names meshstack login", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		// A login entry with no refresh token: the file was written by a CLI that stored none.
		loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{}))

		session := resolved(t, ResolveSessionOptions{})
		_, err := session.BearerToken(t.Context())
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
	loginProfile(t, stack.URL.String(), "demo", credential.Credential{
		Login:  &credential.Login{Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old"},
		ApiKey: &credential.ApiKey{Id: "key-42", Secret: testSecret},
	})

	session := resolved(t, ResolveSessionOptions{})
	require.Equal(t, credential.MethodLogin, session.Method())

	_, err := session.BearerToken(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid_grant")
	require.Equal(t, 0, stack.apiLoginCount(), "the API key in the same profile must not have been used")
	require.Equal(t, credential.MethodLogin, session.Method())
}

// TestTheStoredIssuerIsCheckedBeforeTheRefreshGrant holds the stricter of the two endpoint
// checks: it catches a repointed keycloak behind an unchanged meshStack endpoint.
func TestTheStoredIssuerIsCheckedBeforeTheRefreshGrant(t *testing.T) {
	stack := newMeshStack(t)
	isolate(t)
	loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
		Issuer: mustUrl("https://sso.somewhere-else.example.com"), RefreshToken: "refresh-old",
	}))

	session := resolved(t, ResolveSessionOptions{})
	_, err := session.BearerToken(t.Context())
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

		session := resolved(t, ResolveSessionOptions{})
		require.Empty(t, session.Workspace)
		p := problemOf(t, session.RequireWorkspace())
		require.Equal(t, "no workspace", p.Summary())
		assert.Contains(t, p.Detail(), "--workspace")
		assert.Contains(t, p.Detail(), meshstack.Workspace.EnvKey)
		assert.Contains(t, p.Detail(), "meshstack profile set workspace")
	})

	t.Run("the login method with a workspace passes", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "demo", storedLogin())

		require.NoError(t, resolved(t, ResolveSessionOptions{}).RequireWorkspace())
	})

	t.Run("an API key carries its own", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		require.NoError(t, resolved(t, ResolveSessionOptions{}).RequireWorkspace())
	})

	t.Run("an API token carries its own", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiToken.EnvKey, fakeToken("pasted-token"))

		require.NoError(t, resolved(t, ResolveSessionOptions{}).RequireWorkspace())
	})
}

// TestOnlyTheLoginMethodIsScopedToAWorkspace holds the token cache key: an API key or a
// pasted token carries whatever workspace its issuer put in it, and nothing re-scopes one.
func TestOnlyTheLoginMethodIsScopedToAWorkspace(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "demo", storedLogin())

		require.Equal(t, meshstack.WorkspaceScope("demo"), resolved(t, ResolveSessionOptions{}).Scope())
	})

	t.Run("apiKey", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)
		t.Setenv(meshstack.Workspace.EnvKey, "demo")

		session := resolved(t, ResolveSessionOptions{})
		require.Equal(t, "demo", session.Workspace, "the workspace still resolves; it is a request parameter too")
		require.Equal(t, meshstack.Unscoped, session.Scope())
	})

	t.Run("manual", func(t *testing.T) {
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, "https://api.example.com")
		t.Setenv(credential.ApiToken.EnvKey, fakeToken("pasted-token"))
		t.Setenv(meshstack.Workspace.EnvKey, "demo")

		require.Equal(t, meshstack.Unscoped, resolved(t, ResolveSessionOptions{}).Scope())
	})
}

// TestConcurrentBearerTokenCallsMintOnce holds the rule a Terraform provider depends on: many
// requests in flight at once cost one token, not one each.
func TestConcurrentBearerTokenCallsMintOnce(t *testing.T) {
	run := func(t *testing.T, callers int, stack *fakeMeshStack, session *Session) {
		t.Helper()
		tokens := make([]string, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				token, err := session.BearerToken(t.Context())
				assert.NoError(t, err)
				tokens[i] = token
			}()
		}
		wg.Wait()

		require.Equal(t, 1, stack.apiLoginCount(), "every goroutine but one must have used the token the first minted")
		for _, token := range tokens {
			require.Equal(t, "api-key-token-1", tokenId(token))
		}
	}

	t.Run("a memory store", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		run(t, 32, stack, resolved(t, ResolveSessionOptions{}))
	})

	// The lock is the riskier half: the waiters have to re-read under it and find the
	// winner's token rather than mint one each.
	t.Run("a file store under its lock", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, credential.IssuedToken{})

		run(t, 8, stack, resolved(t, ResolveSessionOptions{}))
	})
}

// TestOnlyTheGrantRunsUnderTheCredentialsLock is what keeps a slow identity provider from
// costing a user their login. The lock is broken as stale after profile's lockStaleAfter, so
// anything under it that can take longer than one token request lets a second process in, and
// two refresh grants on one refresh token end the whole keycloak session. Discovery is two GETs
// on a client that allows a minute each, so it belongs above the lock.
func TestOnlyTheGrantRunsUnderTheCredentialsLock(t *testing.T) {
	stack := newMeshStack(t)
	at := isolate(t)

	lockPath := filepath.Join(at.credentials, testProfile+".json.lock")
	var mu sync.Mutex
	held := map[string]bool{}
	stack.onEachRequest(func(r *http.Request) {
		_, err := os.Stat(lockPath)
		mu.Lock()
		defer mu.Unlock()
		held[r.URL.Path] = err == nil
	})

	loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
		Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old",
	}))

	session := resolved(t, ResolveSessionOptions{})
	_, err := session.BearerToken(t.Context())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.False(t, held["/mesh/info"], "discovery must run before the credentials lock is taken")
	assert.False(t, held["/.well-known/openid-configuration"], "discovery must run before the credentials lock is taken")
	assert.True(t, held["/token"], "the grant is what the lock exists for")
}

// notWritableStore is a read-only credentials directory as pkg/auth meets one. The interesting
// half is that the mint runs first and only the write fails, because by then a browser login
// has already spent a refresh grant keycloak rotated the refresh token for.
type notWritableStore struct {
	profile.Store
	// beforeMint fails the way a lock that cannot be created does: nothing is minted at all.
	beforeMint bool
	updates    int
}

func (s *notWritableStore) Update(ctx context.Context, mint func(profile.Credentials) (profile.Credentials, error)) (profile.Credentials, error) {
	s.updates++
	if !s.beforeMint {
		credentials, err := s.Read()
		if err != nil {
			return profile.Credentials{}, err
		}
		if _, err := mint(credentials); err != nil {
			return profile.Credentials{}, err
		}
	}
	return profile.Credentials{}, errors.Join(errors.New("the credentials directory is read-only"), profile.ErrNotWritable)
}

// TestAStoreThatCannotBeWrittenKeepsWhatItMinted holds the other half of the same session: the
// store mints and then writes, so a failed write has a token in hand already, and granting
// again would replay the refresh token that grant rotated — one process ending its own login,
// with no second process involved.
func TestAStoreThatCannotBeWrittenKeepsWhatItMinted(t *testing.T) {
	withLogin := func(t *testing.T, stack *fakeMeshStack) *Session {
		t.Helper()
		isolate(t)
		loginProfile(t, stack.URL.String(), "demo", credential.FromLogin(credential.Login{
			Issuer: mustUrl(stack.URL.String()), RefreshToken: "refresh-old",
		}))
		return resolved(t, ResolveSessionOptions{})
	}

	t.Run("a write that failed after the grant does not grant again", func(t *testing.T) {
		stack := newMeshStack(t)
		session := withLogin(t, stack)
		store := &notWritableStore{Store: session.store}
		session.store = store

		token, err := session.BearerToken(t.Context())
		require.NoError(t, err, "the token was minted; only persisting it failed")
		require.Equal(t, 1, stack.refreshCount(), "a second grant would replay the refresh token the first one rotated")
		require.Equal(t, 1, store.updates)

		// The rotated refresh token has to reach the memory store, because the file still holds
		// the one keycloak retired and renewing from that is the replay.
		degraded := session.currentStore()
		require.False(t, degraded.Writable())
		credentials, err := degraded.Read()
		require.NoError(t, err)
		require.Equal(t, "refresh-1", credentials.Login.RefreshToken)
		require.Equal(t, token, credentials.Login.AccessTokens[meshstack.WorkspaceScope("demo")].Token.String)
	})

	t.Run("a failure before the grant still mints once after degrading", func(t *testing.T) {
		stack := newMeshStack(t)
		session := withLogin(t, stack)
		store := &notWritableStore{Store: session.store, beforeMint: true}
		session.store = store

		_, err := session.BearerToken(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, stack.refreshCount(), "nothing was minted under the lock, so the memory store mints once")
		require.Equal(t, 1, store.updates, "the unwritable store is used once and then replaced")
	})
}

// TestConcurrentRefreshCallsMintOnce is the same rule for the 401 path, and it is the one that
// needs the rejected token: every request in flight is refused the same token at the same
// moment, and only the token they name is ruled out. A waiter that re-read under the lock and
// distrusted the whole store would mint one token each — for a browser login, one refresh grant
// each, on a refresh token keycloak lets be reused once.
func TestConcurrentRefreshCallsMintOnce(t *testing.T) {
	run := func(t *testing.T, callers int, stack *fakeMeshStack, session *Session) {
		t.Helper()
		rejected, err := session.BearerToken(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, stack.apiLoginCount())

		tokens := make([]string, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				token, err := session.RefreshBearerToken(t.Context(), rejected)
				assert.NoError(t, err)
				tokens[i] = token
			}()
		}
		wg.Wait()

		require.Equal(t, 2, stack.apiLoginCount(), "one 401 on a shared token costs one token, not one each")
		for _, token := range tokens {
			require.Equal(t, "api-key-token-2", tokenId(token))
		}
	}

	t.Run("a memory store", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		t.Setenv(meshstack.Endpoint.EnvKey, stack.URL.String())
		t.Setenv(credential.ApiKeyId.EnvKey, "key-42")
		t.Setenv(credential.ApiSecret.EnvKey, testSecret)

		run(t, 32, stack, resolved(t, ResolveSessionOptions{}))
	})

	t.Run("a file store under its lock", func(t *testing.T) {
		stack := newMeshStack(t)
		isolate(t)
		apiKeyProfile(t, stack, credential.IssuedToken{})

		run(t, 8, stack, resolved(t, ResolveSessionOptions{}))
	})
}
