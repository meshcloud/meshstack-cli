package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// graceWindow is how much life a token must have left to count as valid. It covers a request
// issued just moments before expiry as well as modest clock skew against the identity
// provider. It deliberately does not cover a badly wrong clock — see
// client.Authorization.RefreshBearerToken for what does.
const graceWindow = 30 * time.Second

// BearerToken produces the token for the Authorization header, renewing it when neither cache
// holds a valid one. It runs before every HTTP request, so it does no I/O while the in-process
// token is still good. Ruling nothing out is what makes it the common path.
func (s *Session) BearerToken(ctx context.Context) (string, error) {
	return s.RefreshBearerToken(ctx, "")
}

// RefreshBearerToken implements client.Authorization, and is the whole of BearerToken as well:
// it answers with a valid token for this session's scope that is not the rejected one, so a 401
// on a token both caches still believe in mints a new one exactly once.
//
// Ruling out that one token is all the two methods differ by, and it is what a session would
// otherwise have to remember. A request refused a token another goroutine has already replaced
// needs no mint at all: the replacement is in the cache, it is not the ruled out one, and it
// comes back without any I/O. That matters most for a browser login, where every mint spends a
// refresh grant that rotates the refresh token.
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
	if s.current != method.Login {
		return workspace.Unscoped
	}
	return s.Workspace.Scope()
}

// Method reports which method mints for this session.
func (s *Session) Method() method.Method {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// RequireWorkspace fails before any request is made when a browser login has no workspace to
// scope its token to, because an unscoped user token reaches almost nothing and meshfed's own
// message names neither the flag nor the profile setting.
//
// A command calls it when it acts on meshObjects. `meshstack workspace list` and
// `meshstack auth status` do not, because they are what a user needs in order to pick one.
func (s *Session) RequireWorkspace() error {
	if s.Method() == method.Login && s.Workspace.Empty() {
		return diags.Errorf("no workspace", "%s", workspace.ErrMissing)
	}
	return nil
}

// degradeToMemory keeps a machine with no writable home directory usable: the token lives for
// this process and is re-minted by the next one, which is what a container gets anyway.
//
// It takes over the credentials the failed write was carrying, rather than re-reading the
// file, because a refresh grant rotates before the write happens: the file's copy of the
// refresh token is the one keycloak has already retired, and renewing from it later is the
// replay that ends the session.
//
// It fails instead when this process can interact with a person, because a `meshstack auth
// login` that cannot save has done pointless work and stopping early is a kindness. A CI job
// cannot act on a warning, so it gets a debug record and carries on.
func (s *Session) degradeToMemory(cause error, renewed renewal) error {
	if tty.IsInteractive() {
		return diags.Wrap(cause, "cannot write this profile's credentials",
			"%s could not be written: %v. Fix the permissions, or supply a credential through %s and %s instead.",
			s.currentStore().Describe(), cause, envApiKey, envApiSecret)
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

// unscoped returns a session that mints the unscoped token, sharing this one's store, input and
// discovered configuration, so that a token it obtains is cached and locked exactly like any
// other. It is a fresh value rather than a copy because a Session carries a mutex.
func (s *Session) unscoped() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &Session{
		Endpoint:   s.Endpoint,
		Profile:    s.Profile,
		input:      s.input,
		store:      s.store,
		whole:      s.whole,
		sources:    s.sources,
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

// renewal is what one pass through renew produced. Its credentials are nil only when the mint
// never ran — the lock could not be taken — and otherwise hold what the store was asked to
// write, whether or not the write succeeded: a refresh grant rotates before the write, so what
// the mint returned is the only copy the identity provider still honours.
type renewal struct {
	token       profile.IssuedToken
	credentials *profile.Credentials
}

func (s *Session) renew(ctx context.Context, tokenScope scope.Scope, current method.Method, rejected string) (renewal, error) {
	// Discovery is two GETs on a client that allows a minute per request, and it happens above
	// Update so that the credentials lock covers the token grant and nothing else. A hold that
	// outlasts profile's lockStaleAfter is broken by the next process, and the two then run a
	// refresh grant each on one refresh token. Only the login method refreshes, so an API key
	// still pays for no discovery at all.
	var config oidc.Client
	if current == method.Login {
		discovered, err := s.discover(ctx)
		if err != nil {
			return renewal{}, err
		}
		config = discovered
	}

	var out renewal
	var mintErr error
	_, err := s.currentStore().Update(ctx, func(credentials profile.Credentials) (profile.Credentials, error) {
		// Re-read under the lock: another process, or another goroutine that was already
		// waiting on it, may have renewed meanwhile, in which case its token is used and
		// nothing else happens. Only the rejected token itself is refused here — a 401 says
		// nothing about a token that was minted after it.
		if stored, ok := credentials.AccessTokens[tokenScope]; ok && valid(stored) && stored.Token.String != rejected {
			out = renewal{token: stored, credentials: &credentials}
			return credentials, nil
		}
		var updated profile.Credentials
		updated, out.token, mintErr = s.mint(ctx, config, credentials, current, tokenScope)
		out.credentials = &updated
		// A failed mint still writes what it changed, and the error travels beside the
		// credentials rather than inside them. The refresh grant is why: keycloak rotates the
		// refresh token before anything else can go wrong, so a mint that rotates and then
		// fails the workspace check must not leave the old token on disk — one reuse is
		// tolerated and the next one ends the whole session.
		return updated, nil
	})
	if err == nil {
		err = mintErr
	}
	if err != nil {
		// What was minted travels with the error, because a write that failed has already spent
		// the grant that produced it.
		return out, err
	}
	if !valid(out.token) {
		return out, s.deadMethodError(current)
	}
	return out, nil
}

// mint obtains a fresh access token from the current method, and never from another one. It
// runs under the credentials lock, so the one request it may make is the grant itself: config
// is discovered above, and is the zero value for the methods that need none.
func (s *Session) mint(ctx context.Context, config oidc.Client, credentials profile.Credentials, current method.Method, tokenScope scope.Scope) (profile.Credentials, profile.IssuedToken, error) {
	switch current {
	case method.Manual:
		return s.mintManual(ctx, credentials, tokenScope)
	case method.ApiKey:
		return s.mintApiKey(ctx, credentials, tokenScope)
	case method.Login:
		return s.mintLogin(ctx, config, credentials, tokenScope)
	default:
		return credentials, profile.IssuedToken{}, diags.Errorf("unknown authentication method",
			"the profile records %q, which this version of the meshStack CLI does not know.", current)
	}
}

func (s *Session) mintManual(ctx context.Context, credentials profile.Credentials, tokenScope scope.Scope) (profile.Credentials, profile.IssuedToken, error) {
	// A stored API token has nothing behind it to mint from, so the caller's dead-method
	// message is the right outcome. Only a token that arrived whole from the environment or a
	// provider block is fetched here, and then it is fetched afresh each time, which is what
	// makes a memory store cost no files.
	if !s.whole {
		return credentials, credentials.AccessTokens[tokenScope], nil
	}
	token, err := s.input.ApiToken(ctx)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	if strings.TrimSpace(token) == "" {
		return credentials, profile.IssuedToken{}, diags.Errorf("no meshStack API token",
			"%s is set but empty.", envApiToken)
	}
	parsed, err := jwt.Parse(token)
	if err != nil {
		return credentials, profile.IssuedToken{}, diags.Wrap(err, "this is not a meshStack API token",
			"what %s supplied could not be read as an access token: %v", envApiToken, err)
	}
	issued := issuedToken(parsed)
	return withToken(credentials, tokenScope, issued), issued, nil
}

func (s *Session) mintApiKey(ctx context.Context, credentials profile.Credentials, tokenScope scope.Scope) (profile.Credentials, profile.IssuedToken, error) {
	apiKey := credentials.Methods.ApiKey
	if apiKey == nil || apiKey.ClientId == "" {
		return credentials, profile.IssuedToken{}, diags.Errorf("no meshStack API key",
			"this profile's current method is an API key, but it holds no key id. Run `meshstack auth login --api-key=<id>`.")
	}
	secret, err := s.apiKeySecret(ctx, apiKey)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	token, err := apiLogin(ctx, s.Endpoint, apiKey.ClientId, secret)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	issued := issuedToken(token)
	slog.Debug("minted an access token from an API key", "clientId", apiKey.ClientId, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, tokenScope, issued), issued, nil
}

func (s *Session) mintLogin(ctx context.Context, config oidc.Client, credentials profile.Credentials, tokenScope scope.Scope) (profile.Credentials, profile.IssuedToken, error) {
	login := credentials.Methods.Login
	if login == nil || login.RefreshToken == "" {
		return credentials, profile.IssuedToken{}, s.deadMethodError(method.Login)
	}
	// Stricter than the endpoint check on the file, and it catches a repointed keycloak
	// behind an unchanged endpoint — but it exists only where a login method does, which is
	// why it cannot replace the endpoint check.
	if login.Issuer != nil && login.Issuer.String() != config.Issuer.String() {
		return credentials, profile.IssuedToken{}, diags.Errorf("this login belongs to a different identity provider",
			"the stored login came from %s, but %s now reports %s. Run `meshstack login` to log in again.",
			login.Issuer, s.Endpoint, config.Issuer)
	}

	refreshed, err := config.Refresh(ctx, login.RefreshToken, s.Workspace)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	// A workspace the user is not in yields a token rather than an error: it comes back
	// without MC_CUSTOMER and with an empty group list, and the next API call then fails on
	// permissions. Checking the claim here is what turns that into a message naming the
	// workspace. It is not a security check, and the signature is not verified.
	login.RefreshToken = refreshed.RefreshToken
	credentials.Methods.Login = login
	if !s.Workspace.Empty() {
		if got := jwt.WorkspaceClaim.GetFrom(refreshed.AccessToken); got != s.Workspace {
			// The rotated refresh token goes back either way — the grant already succeeded, so
			// keycloak has invalidated the one on disk whatever this check says.
			return credentials, profile.IssuedToken{}, diags.Errorf("this login cannot act in that workspace",
				"the identity provider issued a token for %q that carries no membership of it. `meshstack workspace list` shows the workspaces you can use.",
				s.Workspace)
		}
	}

	issued := issuedToken(refreshed.AccessToken)
	slog.Debug("minted an access token from the browser login", "scope", tokenScope, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, tokenScope, issued), issued, nil
}

// apiKeySecret answers where the secret comes from. The environment sits above the profile,
// so a set MESHSTACK_API_SECRET reaches the front end's accessor and wins; otherwise a stored
// secret or a secret command is used; otherwise the front end prompts.
func (s *Session) apiKeySecret(ctx context.Context, apiKey *profile.ApiKeyMethod) (string, error) {
	stored := apiKey.ClientSecret != "" || len(apiKey.ClientSecretCommand) > 0
	if env(envApiSecret) != "" || !stored {
		secret, err := s.input.ApiKeySecret(ctx)
		if err != nil {
			return "", err
		}
		return secret, profile.CheckSecret(secret)
	}
	secret, err := apiKey.Secret(ctx)
	if err != nil {
		return "", err
	}
	return secret, profile.CheckSecret(secret)
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

// deadMethodError names the way out. Renewal never switches method, so when the current one
// cannot mint, the command fails and says what to do.
func (s *Session) deadMethodError(current method.Method) error {
	switch current {
	case method.Manual:
		return diags.Errorf("this API token has expired",
			"nothing can refresh an API token. Set %s to a fresh one, or store one with `meshstack auth login --api-token`.", envApiToken)
	case method.ApiKey:
		clientId := "<id>"
		if credentials, err := s.currentStore().Read(); err == nil && credentials.Methods.ApiKey != nil && credentials.Methods.ApiKey.ClientId != "" {
			clientId = credentials.Methods.ApiKey.ClientId
		}
		return diags.Errorf("this API key no longer works",
			"the key was deleted, or its secret changed. Run `meshstack auth login --api-key=%s`.", clientId)
	default:
		return diags.Errorf("this login has expired or was revoked",
			"a meshStack CLI login lasts at most 24 hours. Run `meshstack login`.")
	}
}

// issuedToken carries an access token into the store with the deadline it states itself. A
// token that states none is stored with a zero expiry, which valid reads as "the server
// decides", and that is the honest answer for one nothing can renew.
func issuedToken(token jwt.JWT) profile.IssuedToken {
	issued := profile.IssuedToken{Token: token}
	if expiry := jwt.Expiry.GetFrom(token); expiry != nil {
		issued.ExpiresAt = *expiry
	}
	return issued
}

func withToken(credentials profile.Credentials, tokenScope scope.Scope, token profile.IssuedToken) profile.Credentials {
	if credentials.AccessTokens == nil {
		credentials.AccessTokens = map[scope.Scope]profile.IssuedToken{}
	}
	credentials.AccessTokens[tokenScope] = token
	return credentials
}

func valid(token profile.IssuedToken) bool {
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
