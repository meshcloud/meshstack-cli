package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// graceWindow is how much life a token must have left to count as valid. It covers a request
// issued just moments before expiry as well as modest clock skew against the identity
// provider. It deliberately does not cover a badly wrong clock — see client.TokenRejector for
// what does.
const graceWindow = 30 * time.Second

// Header produces the Authorization header, renewing the token when neither cache holds a
// valid one. It runs before every HTTP request, so it does no I/O while the in-process token
// is still good.
func (s *Session) Header(ctx context.Context) (string, error) {
	token, err := s.token(ctx)
	if err != nil {
		return "", err
	}
	return "Bearer " + token.Token, nil
}

// Rejected implements client.TokenRejector: the header this session produced came back 401,
// so the next Header call re-mints instead of trusting either cache.
func (s *Session) Rejected(header string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached.Token == "" || header != "Bearer "+s.cached.Token {
		return
	}
	slog.Debug("the meshStack API rejected a token this process believed valid, re-minting once")
	s.cached = profile.IssuedToken{}
	s.remint = true
}

// Scope is the key this session's tokens are cached under. Only a browser login is scoped to
// a workspace: an API key or a pasted token carries whatever workspace its issuer put in it,
// and nothing re-scopes one.
func (s *Session) Scope() workspace.Scope {
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
	if s.Method() == method.Login && s.Workspace == "" {
		return diags.Errorf("no workspace", "%s", workspace.ErrMissing)
	}
	return nil
}

func (s *Session) token(ctx context.Context) (profile.IssuedToken, error) {
	s.mu.Lock()
	cached, remint, current := s.cached, s.remint, s.current
	s.mu.Unlock()
	if !remint && valid(cached) {
		return cached, nil
	}

	scope := s.Scope()
	// Keycloak tolerates one reuse of a refresh token and then ends the whole session, so a
	// "already used" answer almost always means another process rotated it a moment ago.
	// Dropping the lock, re-reading and retrying once finds that process's token.
	for attempt := range 2 {
		minted, err := s.renew(ctx, scope, current, remint)
		switch {
		case err == nil:
			s.mu.Lock()
			s.cached, s.remint = minted, false
			s.mu.Unlock()
			return minted, nil
		case errors.Is(err, oidc.ErrRefreshTokenReused) && attempt == 0:
			slog.Debug("the refresh token was already used, re-reading the store and retrying once")
			remint = false
			continue
		case errors.Is(err, profile.ErrNotWritable) && attempt == 0:
			if err := s.degradeToMemory(err); err != nil {
				return profile.IssuedToken{}, err
			}
			continue
		default:
			return profile.IssuedToken{}, err
		}
	}
	return profile.IssuedToken{}, diags.Errorf("could not renew the meshStack access token",
		"the refresh token kept coming back as already used. Run `meshstack login` to start a new session.")
}

// degradeToMemory keeps a machine with no writable home directory usable: the token lives for
// this process and is re-minted by the next one, which is what a container gets anyway.
//
// It fails instead when this process can interact with a person, because a `meshstack auth
// login` that cannot save has done pointless work and stopping early is a kindness. A CI job
// cannot act on a warning, so it gets a debug record and carries on.
func (s *Session) degradeToMemory(cause error) error {
	if tty.IsInteractive() {
		return diags.Wrap(cause, "cannot write this profile's credentials",
			"%s could not be written: %v. Fix the permissions, or supply a credential through %s and %s instead.",
			s.currentStore().Describe(), cause, envApiKey, envApiSecret)
	}
	slog.Debug("keeping tokens in memory only", "store", s.currentStore().Describe(), "cause", cause)
	credentials, err := s.currentStore().Read()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.store = profile.NewMemoryStore(credentials)
	s.mu.Unlock()
	return nil
}

// currentStore reads the store under the mutex, because degradeToMemory can replace it while
// a Terraform provider has several requests in flight.
func (s *Session) currentStore() profile.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store
}

func (s *Session) renew(ctx context.Context, scope workspace.Scope, current method.Method, remint bool) (profile.IssuedToken, error) {
	var minted profile.IssuedToken
	_, err := s.currentStore().Update(ctx, func(credentials profile.Credentials) (profile.Credentials, error) {
		// Re-read under the lock: another process may have renewed while this one waited, in
		// which case its token is used and nothing else happens.
		if !remint {
			if stored, ok := credentials.AccessTokens[scope]; ok && valid(stored) {
				minted = stored
				return credentials, nil
			}
		}
		updated, token, err := s.mint(ctx, credentials, current, scope)
		minted = token
		return updated, err
	})
	if err != nil {
		return profile.IssuedToken{}, err
	}
	if !valid(minted) {
		return profile.IssuedToken{}, s.deadMethodError(current)
	}
	return minted, nil
}

// mint obtains a fresh access token from the current method, and never from another one.
func (s *Session) mint(ctx context.Context, credentials profile.Credentials, current method.Method, scope workspace.Scope) (profile.Credentials, profile.IssuedToken, error) {
	switch current {
	case method.Manual:
		return s.mintManual(ctx, credentials, scope)
	case method.ApiKey:
		return s.mintApiKey(ctx, credentials, scope)
	case method.Login:
		return s.mintLogin(ctx, credentials, scope)
	default:
		return credentials, profile.IssuedToken{}, diags.Errorf("unknown authentication method",
			"the profile records %q, which this version of the meshStack CLI does not know.", current)
	}
}

func (s *Session) mintManual(ctx context.Context, credentials profile.Credentials, scope workspace.Scope) (profile.Credentials, profile.IssuedToken, error) {
	// A stored API token has nothing behind it to mint from, so the caller's dead-method
	// message is the right outcome. Only a token that arrived whole from the environment or a
	// provider block is fetched here, and then it is fetched afresh each time, which is what
	// makes a memory store cost no files.
	if !s.whole {
		return credentials, credentials.AccessTokens[scope], nil
	}
	token, err := s.input.ApiToken(ctx)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	if strings.TrimSpace(token) == "" {
		return credentials, profile.IssuedToken{}, diags.Errorf("no meshStack API token",
			"%s is set but empty.", envApiToken)
	}
	issued := profile.IssuedToken{Token: token}
	if expiry, ok := oidc.Expiry(token); ok {
		issued.ExpiresAt = expiry
	}
	return withToken(credentials, scope, issued), issued, nil
}

func (s *Session) mintApiKey(ctx context.Context, credentials profile.Credentials, scope workspace.Scope) (profile.Credentials, profile.IssuedToken, error) {
	apiKey := credentials.Methods.ApiKey
	if apiKey == nil || apiKey.ClientId == "" {
		return credentials, profile.IssuedToken{}, diags.Errorf("no meshStack API key",
			"this profile's current method is an API key, but it holds no key id. Run `meshstack auth login --api-key=<id>`.")
	}
	secret, err := s.apiKeySecret(ctx, apiKey)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	token, lifetime, err := apiLogin(ctx, s.Endpoint, apiKey.ClientId, secret)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	issued := profile.IssuedToken{Token: token, ExpiresAt: time.Now().Add(lifetime)}
	slog.Debug("minted an access token from an API key", "clientId", apiKey.ClientId, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, scope, issued), issued, nil
}

func (s *Session) mintLogin(ctx context.Context, credentials profile.Credentials, scope workspace.Scope) (profile.Credentials, profile.IssuedToken, error) {
	login := credentials.Methods.Login
	if login == nil || login.RefreshToken == "" {
		return credentials, profile.IssuedToken{}, s.deadMethodError(method.Login)
	}
	config, err := s.discover(ctx)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	// Stricter than the endpoint check on the file, and it catches a repointed keycloak
	// behind an unchanged endpoint — but it exists only where a login method does, which is
	// why it cannot replace the endpoint check.
	if login.Issuer != "" && login.Issuer != config.Issuer {
		return credentials, profile.IssuedToken{}, diags.Errorf("this login belongs to a different identity provider",
			"the stored login came from %s, but %s now reports %s. Run `meshstack login` to log in again.",
			login.Issuer, s.Endpoint, config.Issuer)
	}

	refreshToken, accessToken, lifetime, err := oidc.Refresh(ctx, config, login.RefreshToken, s.Workspace)
	if err != nil {
		return credentials, profile.IssuedToken{}, err
	}
	// A workspace the user is not in yields a token rather than an error: it comes back
	// without MC_CUSTOMER and with an empty group list, and the next API call then fails on
	// permissions. Checking the claim here is what turns that into a message naming the
	// workspace. It is not a security check, and the signature is not verified.
	if s.Workspace != "" {
		if got := oidc.ClaimWorkspace(accessToken); got != s.Workspace {
			return credentials, profile.IssuedToken{}, diags.Errorf("this login cannot act in that workspace",
				"the identity provider issued a token for %q that carries no membership of it. `meshstack workspace list` shows the workspaces you can use.",
				s.Workspace)
		}
	}

	login.RefreshToken = refreshToken
	credentials.Methods.Login = login
	issued := profile.IssuedToken{Token: accessToken, ExpiresAt: time.Now().Add(lifetime)}
	slog.Debug("minted an access token from the browser login", "scope", scope, "expiresAt", issued.ExpiresAt)
	return withToken(credentials, scope, issued), issued, nil
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
func (s *Session) discover(ctx context.Context) (oidc.ClientConfig, error) {
	s.mu.Lock()
	cached := s.oidcConfig
	s.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	config, err := oidc.Discover(ctx, s.Endpoint)
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

func withToken(credentials profile.Credentials, scope workspace.Scope, token profile.IssuedToken) profile.Credentials {
	if credentials.AccessTokens == nil {
		credentials.AccessTokens = map[workspace.Scope]profile.IssuedToken{}
	}
	credentials.AccessTokens[scope] = token
	return credentials
}

func valid(token profile.IssuedToken) bool {
	if token.Token == "" {
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
