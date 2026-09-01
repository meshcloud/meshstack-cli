package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

type LoginOptions struct {
	// Force skips the probe of the stored login, which is how a half-broken profile is fixed
	// without deleting files by hand. Choosing a method implies it, because changing method
	// is explicit by nature.
	Force bool

	// ChooseWorkspace is nil where the front end cannot ask — the Terraform provider, or a
	// CLI with no terminal — and the login then leaves the profile's default alone.
	ChooseWorkspace func(ctx context.Context, candidates []string) (string, error)

	// Browser is nil where the front end has no way to open one, and a login that has to
	// create a session then fails instead.
	Browser Browser
}

// LoginResult is the facts rather than a sentence, so the CLI and the Terraform provider can
// word them their own way.
type LoginResult struct {
	Method    credential.Method
	Endpoint  string
	Profile   string
	Workspace string

	// AlreadyLoggedIn reports that the stored login still worked, so no browser was opened.
	AlreadyLoggedIn bool
	// SwitchedFrom is empty when nothing changed.
	SwitchedFrom credential.Method
	// Username is the preferred_username claim of the token that was obtained.
	Username string
	// ExpiresAt is the deadline of a stored API token, the one credential with a deadline
	// nothing can extend. ExpiryKnown is false for a token that carries no exp claim, which
	// is stored with an unknown expiry rather than a guessed one.
	ExpiresAt   time.Time
	ExpiryKnown bool
}

// Login is the only thing that switches method, and switching discards every cached access
// token, because those tokens carry the old identity.
func (s *Session) Login(ctx context.Context, options LoginOptions) (LoginResult, error) {
	demanded := s.Method()
	return s.login(ctx, demanded, func(result *LoginResult) error {
		switch demanded {
		case credential.MethodManual:
			return s.loginWithApiToken(ctx, result)
		case credential.MethodApiKey:
			return s.loginWithApiKey(ctx, result)
		default:
			return s.loginWithBrowser(ctx, options, result)
		}
	})
}

// login is the bookkeeping every login shares, around the one exchange that differs.
//
// Switching method deliberately does not force a fresh login: switching back to a browser
// login costs one refresh rather than a new browser session, which is what makes a profile
// shared between CI and a developer usable. `--api-key` and `--api-token` set Force
// themselves, because for them forcing only means "do the exchange again".
func (s *Session) login(ctx context.Context, demanded credential.Method, exchange func(*LoginResult) error) (LoginResult, error) {
	result := LoginResult{Method: demanded, Endpoint: s.Endpoint.String(), Profile: s.Profile, Workspace: s.Workspace}

	credentials, err := s.currentStore().Read()
	if err != nil {
		return result, err
	}
	if credentials.Current != "" && credentials.Current != demanded {
		result.SwitchedFrom = credentials.Current
	}

	if err := exchange(&result); err != nil {
		return result, err
	}

	// Warned only once the switch has happened: a warning after a failed login would send the
	// user looking for tokens that are still there.
	if result.SwitchedFrom != "" {
		slog.WarnContext(ctx, "switching authentication method",
			"detail", fmt.Sprintf("profile %q was using its %s; switched to the %s and discarded the cached tokens. `meshstack login` switches back.",
				s.Profile, result.SwitchedFrom.Description(), demanded.Description()))
	}

	s.mu.Lock()
	s.current, s.cached = demanded, credential.IssuedToken{}
	s.mu.Unlock()
	return result, nil
}

// loginWithBrowser probes the stored login first: `auth login` belongs in setup scripts and
// README instructions, so re-running it must not pop a browser every time.
func (s *Session) loginWithBrowser(ctx context.Context, options LoginOptions, result *LoginResult) error {
	config, err := s.discover(ctx)
	if err != nil {
		return err
	}

	credentials, err := s.currentStore().Read()
	if err != nil {
		return err
	}
	if !options.Force && credentials.Login != nil && credentials.Login.RefreshToken != "" {
		if err := s.probeLogin(ctx, config, result); err == nil {
			result.AlreadyLoggedIn = true
			return s.chooseWorkspace(ctx, options, result)
		} else {
			slog.Debug("the stored login did not work, opening a browser", "error", err)
		}
	}

	if options.Browser == nil {
		return diags.Errorf("this front end cannot create a login",
			"the stored login is gone and nothing here can start a new one. Run `meshstack login`, or use an API key.")
	}
	// A terminal is deliberately not required: the URL goes to stderr, which reaches a person
	// from a pipe as well. Only somebody saying nobody is watching refuses this.
	if s.noInput {
		return diags.Errorf("cannot wait for a browser login",
			"%s says nobody is here to visit the login URL. Use `meshstack auth login --api-key=<id>` instead.", tty.NoInputHint())
	}

	token, err := options.Browser(ctx, config)
	if err != nil {
		return err
	}
	result.Username = jwt.UsernameClaim.GetFrom(token.AccessToken)

	// The unscoped access token goes into the store alongside the refresh token in one write,
	// because keycloak rotates on every refresh and separating the two ends the session.
	if _, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = &s.Endpoint
		c.Credential = switchTo(c.Credential, credential.FromLogin(credential.Login{
			Issuer:       &config.Issuer,
			RefreshToken: token.RefreshToken,
			// Nothing in the token says when the login dies, while the server caps the
			// session at 24 hours. Recording when it happened and reporting its age beats
			// predicting a deadline from a constant in another repository.
			ObtainedAt: time.Now().UTC(),
			AccessTokens: map[scope.Scope]credential.IssuedToken{
				meshstack.Unscoped: issuedToken(token.AccessToken),
			},
		}))
		return c, nil
	}); err != nil {
		return err
	}
	return s.chooseWorkspace(ctx, options, result)
}

func (s *Session) probeLogin(ctx context.Context, config oidc.Client, result *LoginResult) error {
	var probed jwt.JWT
	_, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		token, err := config.Refresh(ctx, c.Login.RefreshToken, "")
		if err != nil {
			return c, err
		}
		probed = token.AccessToken
		login := *c.Login
		login.RefreshToken = token.RefreshToken
		c.Credential = switchTo(c.Credential, credential.FromLogin(login))
		return withToken(c, credential.MethodLogin, meshstack.Unscoped, issuedToken(token.AccessToken)), nil
	})
	if err != nil {
		return err
	}
	result.Username = jwt.UsernameClaim.GetFrom(probed)
	return nil
}

// chooseWorkspace lists the workspaces with the unscoped token the login just obtained,
// which is the only thing an unscoped user token is good for.
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

func (s *Session) rememberWorkspace(name string, result *LoginResult) error {
	result.Workspace = name
	if s.Profile == "" {
		return nil
	}
	return profile.SetWorkspace(s.Profile, name)
}

func (s *Session) loginWithApiKey(ctx context.Context, result *LoginResult) error {
	// The resolution already paired an id with a secret, and refused with ErrNoApiSecret if it
	// could not. What is left here is the exchange and the write.
	offered := s.resolved.ApiKey
	if offered == nil || offered.Id == "" {
		return diags.Errorf("no API key id",
			"name one with --api-key=<id> or %s. The id is the only part of a credential that may appear on a command line.",
			credential.ApiKeyId.EnvKey)
	}
	secret, err := offered.Resolve(ctx)
	if err != nil {
		return err
	}
	if err := credential.CheckSecret(secret); err != nil {
		return err
	}
	token, err := apiLogin(ctx, s.Endpoint, offered.Id, secret)
	if err != nil {
		return err
	}

	// A key whose secret this profile already knows how to produce keeps saying so; anything
	// else is stored with the secret that just worked, and never with the token it carried in.
	keeping := &credential.ApiKey{Id: offered.Id, Secret: secret}
	if len(offered.SecretCommand) > 0 {
		keeping = &credential.ApiKey{Id: offered.Id, SecretCommand: offered.SecretCommand}
	}
	if err := s.storeApiKey(ctx, keeping, token); err != nil {
		return err
	}
	result.Username = offered.Id
	return nil
}

// storeApiKey is the one place that decides what an API key credential looks like on disk,
// and it writes the method and the token it just minted in one update.
func (s *Session) storeApiKey(ctx context.Context, apiKey *credential.ApiKey, token jwt.JWT) error {
	_, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = &s.Endpoint
		key := *apiKey
		key.AccessToken = issuedToken(token)
		c.Credential = switchTo(c.Credential, credential.FromApiKey(key))
		return c, nil
	})
	return err
}

func (s *Session) loginWithApiToken(ctx context.Context, result *LoginResult) error {
	// Resolved and parsed already, and refused with ErrNoApiToken if nothing supplied one.
	if s.resolved.Manual == nil {
		return diags.Wrap(ErrNoApiToken, "no meshStack API token", "nothing supplied a token to store.")
	}
	issued := s.resolved.Manual.AccessToken
	if expiry := jwt.Expiry.GetFrom(issued.Token); expiry != nil {
		result.ExpiresAt, result.ExpiryKnown = *expiry, true
	}
	result.Username = jwt.UsernameClaim.GetFrom(issued.Token)

	_, err := s.currentStore().Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
		c.Version = profile.Version
		c.Endpoint = &s.Endpoint
		c.Credential = switchTo(c.Credential, credential.FromManual(credential.Manual{AccessToken: issued}))
		return c, nil
	})
	return err
}

// switchTo keeps the methods it replaces, so that switching back costs one refresh, but drops
// their cached access tokens: a token carries the identity of the method that minted it. A
// pasted token is not kept, because the token is the whole of that method.
func switchTo(previous, selected credential.Credential) credential.Credential {
	if selected.Login == nil && previous.Login != nil {
		login := *previous.Login
		login.AccessTokens = nil
		selected.Login = &login
	}
	if selected.ApiKey == nil && previous.ApiKey != nil {
		apiKey := *previous.ApiKey
		apiKey.AccessToken = credential.IssuedToken{}
		selected.ApiKey = &apiKey
	}
	return selected
}

// Logout removes the profile's credentials file. There is no per-method logout: the file is
// the login. Plain logout is local only; `--revoke` also ends the session at the provider,
// because the endpoints that list and revoke CLI logins live on meshStack's internal API and
// a CLI token cannot reach them — meshPanel's Profile → CLI Logins page is the other way.
func (s *Session) Logout(ctx context.Context, revoke bool) error {
	if revoke {
		credentials, err := s.currentStore().Read()
		if err != nil {
			return err
		}
		if credentials.Login != nil && credentials.Login.RefreshToken != "" {
			config, err := s.discover(ctx)
			if err != nil {
				return err
			}
			if err := config.EndSession(ctx, credentials.Login.RefreshToken); err != nil {
				// A refusal means the session is already gone, which is what the user asked
				// for. Anything else leaves it alive, and a silent logout would hide that.
				if _, refused := errors.AsType[client.HttpError](err); !refused {
					return err
				}
				slog.DebugContext(ctx, "the identity provider reports the session was already gone", "error", err)
			}
		}
	}
	s.mu.Lock()
	s.cached, s.current = credential.IssuedToken{}, ""
	s.mu.Unlock()
	return s.currentStore().Forget()
}
