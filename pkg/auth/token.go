package auth

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

// graceWindow is how much life a token must have left to count as valid. It covers a request
// issued just moments before expiry as well as modest clock skew against the identity
// provider. It deliberately does not cover a badly wrong clock — see
// client.Authorization.RefreshBearerToken for what does.
const graceWindow = 30 * time.Second

// BearerToken runs before every HTTP request, so it does no I/O while the in-process token is
// still good. Ruling nothing out is what makes it the common path.
func (s *Session) BearerToken(ctx context.Context) (string, error) {
	return s.RefreshBearerToken(ctx, "")
}

// RefreshBearerToken implements client.Authorization. Ruling out one token as an argument is
// what saves the session from remembering it: a request refused a token another goroutine has
// already replaced needs no mint at all. That matters most for a browser login, where every
// mint spends a refresh grant that rotates the refresh token.
func (s *Session) RefreshBearerToken(ctx context.Context, rejected string) (string, error) {
	s.mu.Lock()
	cached, current := s.cached, s.current
	s.mu.Unlock()
	if valid(cached) && cached.Token.String != rejected {
		return cached.Token.String, nil
	}
	if valid(cached) {
		slog.Debug("the meshStack API rejected a token this process believed valid, re-minting once")
	}

	tokenScope := s.Scope()
	renewed, err := s.renew(ctx, tokenScope, current, rejected)
	// A store that cannot be written degrades to memory. A store that is merely locked by
	// another process does not: that one holds it for a refresh grant, and minting outside the
	// lock is the replay keycloak ends the session over.
	if errors.Is(err, profile.ErrNotWritable) {
		if err := s.degradeToMemory(err, renewed); err != nil {
			return "", err
		}
		// The store mints before it writes, so the token is usually already in hand when the
		// write fails. Minting again would spend a second refresh grant on a token the first
		// one rotated — one process ending its own session, with no second process involved.
		// Only a failure that came before the grant has nothing to lose by repeating it.
		if valid(renewed.token) {
			err = nil
		} else {
			renewed, err = s.renew(ctx, tokenScope, current, rejected)
		}
	}
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cached = renewed.token
	s.mu.Unlock()
	return renewed.token.Token.String, nil
}

// Scope is the key this session's tokens are cached under. Only a browser login is scoped to
// a workspace: an API key or a pasted token carries whatever workspace its issuer put in it,
// and nothing re-scopes one.
func (s *Session) Scope() scope.Scope {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != credential.MethodLogin {
		return meshstack.Unscoped
	}
	return meshstack.WorkspaceScope(s.Workspace)
}

func (s *Session) Method() credential.Method {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// RequireWorkspace is a post-resolution call, made by a command that acts on meshObjects and
// deliberately not by `meshstack workspace list` or `meshstack auth status` — the two the
// message itself tells the user to run. Folding it into the resolution would make the escape
// hatch it names unreachable.
func (s *Session) RequireWorkspace() error {
	if s.Method() == credential.MethodLogin && s.Workspace == "" {
		return diags.Errorf("no workspace", "%s", meshstack.ErrMissing)
	}
	return nil
}

// degradeToMemory keeps a machine with no writable home directory usable. It takes over the
// credentials the failed write was carrying rather than re-reading the file, because a refresh
// grant rotates before the write: the file's copy is the one keycloak has already retired.
//
// It fails instead when somebody may be watching, because an `auth login` that cannot save has
// done pointless work. A CI job cannot act on a warning, so it carries on.
func (s *Session) degradeToMemory(cause error, renewed renewal) error {
	if !s.noInput {
		return diags.Wrap(cause, "cannot write this profile's credentials",
			"%s could not be written: %v. Fix the permissions, or supply a credential through %s and %s instead.",
			s.currentStore().Describe(), cause, credential.ApiKeyId.EnvKey, credential.ApiSecret.EnvKey)
	}
	slog.Debug("keeping tokens in memory only", "store", s.currentStore().Describe(), "cause", cause)
	credentials := renewed.credentials
	if credentials == nil {
		// The lock was never taken, so nothing was minted and the file is still the truth.
		read, err := s.currentStore().Read()
		if err != nil {
			return err
		}
		credentials = &read
	}
	s.mu.Lock()
	s.store = profile.NewMemoryStore(*credentials)
	s.mu.Unlock()
	return nil
}

// unscoped shares this session's store and discovered configuration, so that a token it
// obtains is cached and locked exactly like any other. It is a fresh value rather than a copy
// because a Session carries a mutex.
func (s *Session) unscoped() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &Session{
		Endpoint:   s.Endpoint,
		Profile:    s.Profile,
		noInput:    s.noInput,
		resolved:   s.resolved,
		store:      s.store,
		current:    s.current,
		oidcConfig: s.oidcConfig,
	}
}

// currentStore reads the store under the mutex, because degradeToMemory can replace it while
// a Terraform provider has several requests in flight.
func (s *Session) currentStore() profile.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store
}

// renewal carries credentials whether or not the write succeeded, and nil only when the mint
// never ran: a refresh grant rotates before the write, so what the mint returned is the only
// copy the identity provider still honours.
type renewal struct {
	token       credential.IssuedToken
	credentials *profile.Credentials
}

func (s *Session) renew(ctx context.Context, tokenScope scope.Scope, current credential.Method, rejected string) (renewal, error) {
	// Above Update, so that the credentials lock covers the token grant and nothing else: a
	// hold that outlasts profile's lockStaleAfter is broken by the next process, and the two
	// then run a refresh grant each on one refresh token.
	var config oidc.Client
	if current == credential.MethodLogin {
		discovered, err := s.discover(ctx)
		if err != nil {
			return renewal{}, err
		}
		config = discovered
	}

	var out renewal
	var mintErr error
	_, err := s.currentStore().Update(ctx, func(credentials profile.Credentials) (profile.Credentials, error) {
		// Re-read under the lock: another process may have renewed meanwhile. Only the
		// rejected token itself is refused here — a 401 says nothing about one minted after it.
		if stored, ok := cachedToken(credentials, current, tokenScope); ok && valid(stored) && stored.Token.String != rejected {
			out = renewal{token: stored, credentials: &credentials}
			return credentials, nil
		}
		var updated profile.Credentials
		updated, out.token, mintErr = s.mint(ctx, config, credentials, current, tokenScope)
		out.credentials = &updated
		// A failed mint still writes what it changed: keycloak rotates the refresh token
		// before anything else can go wrong, so a mint that rotates and then fails the
		// workspace check must not leave the retired token on disk.
		return updated, nil
	})
	if err == nil {
		err = mintErr
	}
	if err != nil {
		return out, err
	}
	if !valid(out.token) {
		return out, s.deadMethodError(current)
	}
	return out, nil
}

// mint runs under the credentials lock, so the one request it may make is the grant itself:
// config is discovered above, and is the zero value for the methods that need none.
func (s *Session) mint(ctx context.Context, config oidc.Client, credentials profile.Credentials, current credential.Method, tokenScope scope.Scope) (profile.Credentials, credential.IssuedToken, error) {
	switch current {
	case credential.MethodManual:
		// The token was resolved and parsed before this session existed, so there is nothing
		// left to mint from: an expired one gets the caller's dead-method message.
		token, _ := cachedToken(credentials, credential.MethodManual, tokenScope)
		return credentials, token, nil
	case credential.MethodApiKey:
		return s.mintApiKey(ctx, credentials, tokenScope)
	case credential.MethodLogin:
		return s.mintLogin(ctx, config, credentials, tokenScope)
	default:
		return credentials, credential.IssuedToken{}, diags.Errorf("unknown authentication method",
			"the profile records %q, which this version of the meshStack CLI does not know.", current)
	}
}

func (s *Session) mintApiKey(ctx context.Context, credentials profile.Credentials, tokenScope scope.Scope) (profile.Credentials, credential.IssuedToken, error) {
	apiKey := credentials.ApiKey
	if apiKey == nil || apiKey.Id == "" {
		return credentials, credential.IssuedToken{}, diags.Errorf("no meshStack API key",
			"this profile's current method is an API key, but it holds no key id. Run `meshstack auth login --api-key=<id>`.")
	}
	// Resolve rather than read: the resolution paired this id with a secret it may only know
	// how to produce, and a clientSecretCommand runs here rather than during a resolution.
	secret, err := apiKey.Resolve(ctx)
	if err != nil {
		return credentials, credential.IssuedToken{}, err
	}
	if err := credential.CheckSecret(secret); err != nil {
		return credentials, credential.IssuedToken{}, err
	}
	token, err := apiLogin(ctx, s.Endpoint, apiKey.Id, secret)
	if err != nil {
		return credentials, credential.IssuedToken{}, err
	}
	issued := issuedToken(token)
	slog.Debug("minted an access token from an API key", "clientId", apiKey.Id, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, credential.MethodApiKey, tokenScope, issued), issued, nil
}

func (s *Session) mintLogin(ctx context.Context, config oidc.Client, credentials profile.Credentials, tokenScope scope.Scope) (profile.Credentials, credential.IssuedToken, error) {
	login := credentials.Login
	if login == nil || login.RefreshToken == "" {
		return credentials, credential.IssuedToken{}, s.deadMethodError(credential.MethodLogin)
	}
	// Stricter than the endpoint check on the file, and it catches a repointed keycloak behind
	// an unchanged endpoint — but it exists only where a login method does.
	if login.Issuer != nil && login.Issuer.String() != config.Issuer.String() {
		return credentials, credential.IssuedToken{}, diags.Errorf("this login belongs to a different identity provider",
			"the stored login came from %s, but %s now reports %s. Run `meshstack login` to log in again.",
			login.Issuer, s.Endpoint, config.Issuer)
	}

	refreshed, err := config.Refresh(ctx, login.RefreshToken, s.Workspace)
	if err != nil {
		return credentials, credential.IssuedToken{}, err
	}
	// A workspace the user is not in yields a token rather than an error, and the next API
	// call then fails on permissions. Checking the claim is what turns that into a message
	// naming the workspace. It is not a security check: no signature is verified.
	updated := *login
	updated.RefreshToken = refreshed.RefreshToken
	credentials.Login = &updated
	if s.Workspace != "" {
		if got := jwt.WorkspaceClaim.GetFrom(refreshed.AccessToken); got != s.Workspace {
			// The rotated refresh token goes back either way — the grant already succeeded, so
			// keycloak has invalidated the one on disk whatever this check says.
			return credentials, credential.IssuedToken{}, diags.Errorf("this login cannot act in that workspace",
				"the identity provider issued a token for %q that carries no membership of it. `meshstack workspace list` shows the workspaces you can use.",
				s.Workspace)
		}
	}

	issued := issuedToken(refreshed.AccessToken)
	slog.Debug("minted an access token from the browser login", "scope", tokenScope, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, credential.MethodLogin, tokenScope, issued), issued, nil
}

// discover reads /mesh/info and the identity provider's configuration, at most once per
// process. Only the login method needs it, so an API key never pays for it.
func (s *Session) discover(ctx context.Context) (oidc.Client, error) {
	s.mu.Lock()
	cached := s.oidcConfig
	s.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	config, err := oidc.NewClient(ctx, http.NewClient(userAgent, nil), s.Endpoint.URL)
	if err != nil {
		return config, err
	}
	s.mu.Lock()
	s.oidcConfig = &config
	s.mu.Unlock()
	return config, nil
}

// deadMethodError names the way out. Renewal never switches method — falling back from a
// browser login to an API key would change the identity behind the command — so when the
// current one cannot mint, the command fails and says what to do.
func (s *Session) deadMethodError(current credential.Method) error {
	switch current {
	case credential.MethodManual:
		return diags.Errorf("this API token has expired",
			"nothing can refresh an API token. Set %s to a fresh one, or store one with `meshstack auth login --api-token`.", credential.ApiToken.EnvKey)
	case credential.MethodApiKey:
		clientId := "<id>"
		if credentials, err := s.currentStore().Read(); err == nil && credentials.ApiKey != nil && credentials.ApiKey.Id != "" {
			clientId = credentials.ApiKey.Id
		}
		return diags.Errorf("this API key no longer works",
			"the key was deleted, or its secret changed. Run `meshstack auth login --api-key=%s`.", clientId)
	default:
		return diags.Errorf("this login has expired or was revoked",
			"a meshStack CLI login lasts at most 24 hours. Run `meshstack login`.")
	}
}

// issuedToken takes the deadline from the token itself. One that states none is stored with a
// zero expiry, which valid reads as "the server decides".
func issuedToken(token jwt.JWT) credential.IssuedToken {
	issued := credential.IssuedToken{Token: token}
	if expiry := jwt.Expiry.GetFrom(token); expiry != nil {
		issued.ExpiresAt = *expiry
	}
	return issued
}

// cachedToken and withToken switch over the three shapes here rather than on
// credential.Credential, because every other branch on the current method is in this package
// too. Only a browser login keys its tokens by scope; for the other two tokenScope is ignored.
func cachedToken(credentials profile.Credentials, current credential.Method, tokenScope scope.Scope) (credential.IssuedToken, bool) {
	switch current {
	case credential.MethodLogin:
		if credentials.Login == nil {
			return credential.IssuedToken{}, false
		}
		token, ok := credentials.Login.AccessTokens[tokenScope]
		return token, ok
	case credential.MethodApiKey:
		if credentials.ApiKey == nil || credentials.ApiKey.AccessToken.Token.String == "" {
			return credential.IssuedToken{}, false
		}
		return credentials.ApiKey.AccessToken, true
	case credential.MethodManual:
		if credentials.Manual == nil || credentials.Manual.AccessToken.Token.String == "" {
			return credential.IssuedToken{}, false
		}
		return credentials.Manual.AccessToken, true
	}
	return credential.IssuedToken{}, false
}

// withToken copies the method it writes to rather than reaching through the pointer, so that
// storing a token cannot change credentials another goroutine is holding.
func withToken(credentials profile.Credentials, current credential.Method, tokenScope scope.Scope, token credential.IssuedToken) profile.Credentials {
	switch current {
	case credential.MethodLogin:
		login := credential.Login{}
		if credentials.Login != nil {
			login = *credentials.Login
		}
		tokens := make(map[scope.Scope]credential.IssuedToken, len(login.AccessTokens)+1)
		maps.Copy(tokens, login.AccessTokens)
		tokens[tokenScope] = token
		login.AccessTokens = tokens
		credentials.Login = &login
	case credential.MethodApiKey:
		apiKey := credential.ApiKey{}
		if credentials.ApiKey != nil {
			apiKey = *credentials.ApiKey
		}
		apiKey.AccessToken = token
		credentials.ApiKey = &apiKey
	case credential.MethodManual:
		manual := credential.Manual{}
		if credentials.Manual != nil {
			manual = *credentials.Manual
		}
		manual.AccessToken = token
		credentials.Manual = &manual
	}
	return credentials
}

func valid(token credential.IssuedToken) bool {
	if token.Token.String == "" {
		return false
	}
	// A zero expiry means the token said nothing about its own life — a pasted API token that
	// is not a JWT is the case. Nothing can renew one, so the server is what decides, and
	// treating it as valid is the only useful answer.
	if token.ExpiresAt.IsZero() {
		return true
	}
	return time.Until(token.ExpiresAt) > graceWindow
}
