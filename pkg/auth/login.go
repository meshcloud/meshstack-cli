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

// LoginOptions are what `meshstack auth login` decides and pkg/auth does not.
type LoginOptions struct {
	// Force skips the probe of the stored login, which is how a half-broken profile is fixed
	// without deleting files by hand. Choosing a method implies it, because changing method
	// is explicit by nature.
	Force bool

	// ChooseWorkspace picks the profile's default workspace from the ones the fresh login can
	// see. It is nil where the front end cannot ask — the Terraform provider, or a CLI with
	// no terminal — and the login then leaves the profile's default alone.
	ChooseWorkspace func(ctx context.Context, candidates []workspace.Name) (workspace.Name, error)
}

// LoginResult is what the front end prints. pkg/auth returns the facts rather than a
// sentence, so the CLI and the Terraform provider can word them their own way.
type LoginResult struct {
	Method    method.Method
	Endpoint  string
	Profile   string
	Workspace workspace.Name

	// AlreadyLoggedIn reports that the stored login still worked, so no browser was opened.
	AlreadyLoggedIn bool
	// SwitchedFrom names the method this login replaced, empty when nothing changed.
	SwitchedFrom method.Method
	// Username is the preferred_username claim of the token that was obtained, when it has one.
	Username string
	// ExpiresAt is the deadline of a stored API token, the one credential with a deadline
	// nothing can extend. ExpiryKnown is false for a token that is not a JWT or carries no
	// exp claim, which is stored with an unknown expiry rather than a guessed one.
	ExpiresAt   time.Time
	ExpiryKnown bool
}

// Login makes the profile hold a working credential and records which method is current.
// It is the only thing that switches method, and switching discards every cached access
// token, because those tokens carry the old identity.
func (s *Session) Login(ctx context.Context, options LoginOptions) (LoginResult, error) {
	demanded := s.Method()
	result := LoginResult{Method: demanded, Endpoint: s.Endpoint.String(), Profile: s.Profile, Workspace: s.Workspace}

	credentials, err := s.currentStore().Read()
	if err != nil {
		return result, err
	}
	if credentials.CurrentMethod != "" && credentials.CurrentMethod != demanded {
		result.SwitchedFrom = credentials.CurrentMethod
		s.input.Warn(diags.Warnf("switching authentication method",
			"profile %q was using its %s; switching to the %s and discarding cached tokens. `meshstack login` switches back.",
			s.Profile, credentials.CurrentMethod.Description(), demanded.Description()))
		options.Force = true
	}

	switch demanded {
	case method.Manual:
		err = s.loginWithApiToken(ctx, &result)
	case method.ApiKey:
		err = s.loginWithApiKey(ctx, &result)
	default:
		err = s.loginWithBrowser(ctx, options, &result)
	}
	if err != nil {
		return result, err
	}

	s.mu.Lock()
	s.current, s.cached, s.remint = demanded, profile.IssuedToken{}, false
	s.mu.Unlock()
	return result, nil
}

// loginWithBrowser probes the stored login before it does anything: `auth login` belongs in
// setup scripts and README instructions, so re-running it must not pop a browser every time.
// The probe is the same renewal every other command makes, and it warms the token cache.
func (s *Session) loginWithBrowser(ctx context.Context, options LoginOptions, result *LoginResult) error {
	config, err := s.discover(ctx)
	if err != nil {
		return err
	}

	credentials, err := s.currentStore().Read()
	if err != nil {
		return err
	}
	if !options.Force && credentials.Methods.Login != nil && credentials.Methods.Login.RefreshToken != "" {
		if err := s.probeLogin(ctx, config, result); err == nil {
			result.AlreadyLoggedIn = true
			return s.chooseWorkspace(ctx, options, result)
		} else {
			slog.Debug("the stored login did not work, opening a browser", "error", err)
		}
	}

	browser := s.input.Browser()
	if browser == nil || !tty.IsInteractive() {
		return diags.Errorf("cannot open a browser here",
			"this process has no way to run an interactive login. Use `meshstack auth login --api-key=<id>` instead.")
	}

	refreshToken, accessToken, lifetime, err := browser.Login(ctx, config)
	if err != nil {
		return err
	}
	result.Username = claimString(accessToken, "preferred_username")

	// The unscoped access token goes into the store alongside the refresh token in one write,
	// because keycloak rotates on every refresh and separating the two ends the session.
	if _, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = s.Endpoint.String()
		c.CurrentMethod = method.Login
		c.Methods.Login = &profile.LoginMethod{
			Issuer:       config.Issuer,
			RefreshToken: refreshToken,
			// Nothing in the token says when the login dies — a refresh token carries no exp
			// and refresh_expires_in is 0 — while the server caps the session at 24 hours. So
			// record when it happened and report its age, rather than predicting a deadline
			// from a constant that lives in another repository.
			ObtainedAt: time.Now().UTC(),
		}
		c.AccessTokens = map[workspace.Scope]profile.IssuedToken{
			workspace.Unscoped: {Token: accessToken, ExpiresAt: time.Now().Add(lifetime)},
		}
		return c, nil
	}); err != nil {
		return err
	}
	return s.chooseWorkspace(ctx, options, result)
}

func (s *Session) probeLogin(ctx context.Context, config oidc.ClientConfig, result *LoginResult) error {
	var probed string
	_, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		refreshToken, accessToken, lifetime, err := oidc.Refresh(ctx, config, c.Methods.Login.RefreshToken, "")
		if err != nil {
			return c, err
		}
		probed = accessToken
		c.Methods.Login.RefreshToken = refreshToken
		c.CurrentMethod = method.Login
		return withToken(c, workspace.Unscoped, profile.IssuedToken{Token: accessToken, ExpiresAt: time.Now().Add(lifetime)}), nil
	})
	if err != nil {
		return err
	}
	result.Username = claimString(probed, "preferred_username")
	return nil
}

// chooseWorkspace stores the profile's default workspace. A browser login lists the user's
// workspaces with the unscoped token it just obtained, which is the only thing an unscoped
// user token is good for.
func (s *Session) chooseWorkspace(ctx context.Context, options LoginOptions, result *LoginResult) error {
	if s.Workspace != "" {
		return s.rememberWorkspace(s.Workspace, result)
	}
	if options.ChooseWorkspace == nil {
		return nil
	}
	candidates, err := s.Workspaces(ctx)
	if err != nil {
		return err
	}
	chosen, err := options.ChooseWorkspace(ctx, candidates)
	if err != nil || chosen == "" {
		return err
	}
	s.Workspace = chosen
	return s.rememberWorkspace(chosen, result)
}

func (s *Session) rememberWorkspace(name workspace.Name, result *LoginResult) error {
	result.Workspace = name
	if s.Profile == "" {
		return nil
	}
	return SetProfileWorkspace(s.Profile, name)
}

func (s *Session) loginWithApiKey(ctx context.Context, result *LoginResult) error {
	credentials, err := s.currentStore().Read()
	if err != nil {
		return err
	}
	stored := credentials.Methods.ApiKey
	clientId := s.input.Explicit().ApiKey
	if clientId == "" {
		clientId = env(envApiKey)
	}
	if clientId == "" && stored != nil {
		// A bare --api-key forces the method with the id already in the profile, which is how
		// a developer tests that CI's key still works from a machine that also holds a
		// browser login.
		clientId = stored.ClientId
	}
	if clientId == "" {
		return diags.Errorf("no API key id",
			"name one with --api-key=<id> or %s. The id is the only part of a credential that may appear on a command line.", envApiKey)
	}

	// A new id must not inherit the old id's secret, so the stored one is only offered when
	// the id is unchanged.
	offered := &profile.ApiKeyMethod{ClientId: clientId}
	if stored != nil && stored.ClientId == clientId {
		offered = stored
	}
	secret, err := s.apiKeySecret(ctx, offered)
	if err != nil {
		return err
	}
	token, lifetime, err := apiLogin(ctx, s.Endpoint, clientId, secret)
	if err != nil {
		return err
	}

	_, err = s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = s.Endpoint.String()
		c.CurrentMethod = method.ApiKey
		if offered.ClientSecret != "" || len(offered.ClientSecretCommand) > 0 {
			c.Methods.ApiKey = offered
		} else {
			c.Methods.ApiKey = &profile.ApiKeyMethod{ClientId: clientId, ClientSecret: secret}
		}
		// Switching method discards every cached access token, because they carry the old
		// identity. The methods themselves survive, so switching back costs one refresh.
		c.AccessTokens = map[workspace.Scope]profile.IssuedToken{
			workspace.Unscoped: {Token: token, ExpiresAt: time.Now().Add(lifetime)},
		}
		return c, nil
	})
	if err != nil {
		return err
	}
	result.Username = clientId
	return nil
}

func (s *Session) loginWithApiToken(ctx context.Context, result *LoginResult) error {
	token, err := s.input.ApiToken(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return diags.Errorf("no API token",
			"supply it through %s or on stdin. A token is never a flag value, because a flag value lands in shell history, in ps output and in CI logs.", envApiToken)
	}
	issued := profile.IssuedToken{Token: strings.TrimSpace(token)}
	if expiry, ok := oidc.Expiry(issued.Token); ok {
		issued.ExpiresAt = expiry
		result.ExpiresAt, result.ExpiryKnown = expiry, true
	}
	result.Username = claimString(issued.Token, "preferred_username")

	_, err = s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = s.Endpoint.String()
		c.CurrentMethod = method.Manual
		c.AccessTokens = map[workspace.Scope]profile.IssuedToken{workspace.Unscoped: issued}
		return c, nil
	})
	return err
}

// Logout removes the profile's credentials file. There is no per-method logout: the file is
// the login.
//
// Plain logout is local only. `--revoke` also ends the session at the identity provider,
// because the endpoints that list and revoke CLI logins live on meshStack's internal API and
// a CLI token cannot reach them — meshPanel's Profile → CLI Logins page is the other way.
func (s *Session) Logout(ctx context.Context, revoke bool) error {
	if revoke {
		credentials, err := s.currentStore().Read()
		if err != nil {
			return err
		}
		if credentials.Methods.Login != nil && credentials.Methods.Login.RefreshToken != "" {
			config, err := s.discover(ctx)
			if err != nil {
				return err
			}
			if err := oidc.EndSession(ctx, config, credentials.Methods.Login.RefreshToken); err != nil {
				// A session that is already gone is the outcome the user asked for, so the
				// local file still goes.
				if !errors.Is(err, oidc.ErrRefreshRejected) {
					return err
				}
				slog.Debug("the identity provider reports the session was already gone", "error", err)
			}
		}
	}
	s.mu.Lock()
	s.cached, s.current = profile.IssuedToken{}, ""
	s.mu.Unlock()
	return s.currentStore().Forget()
}

func claimString(token, name string) string {
	claims, err := oidc.Claims(token)
	if err != nil {
		return ""
	}
	value, _ := claims[name].(string)
	return value
}
