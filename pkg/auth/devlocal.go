package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// devLocalEndpoint is where a local dev stack's meshfed-api listens. It is the only default
// endpoint anywhere in this package, and it belongs to `--dev-local` alone: guessing one for a
// real meshStack would send a credential somewhere nobody named, while guessing one for a
// stack that runs on the developer's own machine is what makes that flag configless.
const devLocalEndpoint = "http://localhost:8080"

// ResolveForDevLocalLogin produces the session `meshstack login --dev-local` works through.
// It is ResolveForLogin with the two defaults that flag brings: the reserved profile name, and
// the local dev stack's endpoint when neither a flag, the environment nor that profile named
// one. Both are applied here rather than in the shared precedence order, because both would be
// wrong for every other command.
func ResolveForDevLocalLogin(ctx context.Context, in Input) (*Session, error) {
	values := in.Explicit()
	if values.Profile == "" && env(envProfile) == "" {
		values.Profile = DevLocalProfile
	}
	if values.Endpoint == "" && env(envEndpoint) == "" && !hasEndpoint(values.Profile) {
		values.Endpoint = devLocalEndpoint
	}
	return ResolveForLogin(ctx, withValues{Input: in, values: values})
}

// LoginDevLocal bootstraps this session's profile from the credentials a local dev stack
// publishes in /mesh/info, so that running against one needs no .env file and no key issued by
// hand. It takes no LoginOptions: there is nothing to force — the exchange happens every time
// — and nothing to choose, because the workspace comes out of the same document.
func (s *Session) LoginDevLocal(ctx context.Context) (LoginResult, error) {
	return s.login(method.ApiKey, func(result *LoginResult) error {
		return s.loginDevLocal(ctx, result)
	})
}

func (s *Session) loginDevLocal(ctx context.Context, result *LoginResult) error {
	// The same unauthenticated fetch pkg/oidc makes during discovery. /mesh/info is public, so
	// this runs before the session holds any credential at all — which is the point.
	info, err := client.NewMeshInfoClient(ctx, s.Endpoint.URL, http.NewClient(userAgent, nil)).Read(ctx)
	if err != nil {
		return diags.Wrap(err, "cannot read this meshStack's public information",
			"%s did not answer /mesh/info: %v", s.Endpoint, err)
	}
	dev := info.DevLocalCredentials
	if dev == nil {
		return diags.Errorf("this meshStack publishes no local dev credentials",
			"%s serves no devLocalCredentials in /mesh/info. meshStack publishes them only on a local dev stack, so --dev-local cannot bootstrap anything here. Use `meshstack login --api-key=<id>` with a key issued in meshPanel instead.",
			s.Endpoint)
	}

	token, err := apiLogin(ctx, s.Endpoint, dev.ApiKeyClientId, dev.ApiKeyClientSecret)
	if err != nil {
		return err
	}
	if err := s.storeApiKey(ctx, &profile.ApiKeyMethod{
		ClientId:     dev.ApiKeyClientId,
		ClientSecret: dev.ApiKeyClientSecret,
	}, token); err != nil {
		return err
	}
	result.Username = dev.ApiKeyClientId

	// The dev stack's key holds ADM_ rights and is therefore not workspace-bound, so nothing
	// here needs a workspace. The profile does: a workspace-scoped command run later fails
	// without one, and on a dev stack the admin workspace is the answer a developer would give.
	if s.Workspace.Empty() {
		s.Workspace = workspace.Name(info.AdminWorkspaceIdentifier)
	}
	if s.Workspace.Empty() {
		return nil
	}
	return s.rememberWorkspace(s.Workspace, result)
}

// withValues overrides what a front end reported explicitly, so that ResolveForDevLocalLogin
// can apply its two defaults and still run the one resolution everything else runs.
type withValues struct {
	Input
	values Values
}

func (w withValues) Explicit() Values { return w.values }

// hasEndpoint reports whether the named profile already carries an endpoint, which is what
// keeps the dev-local default from overriding a profile somebody configured by hand.
func hasEndpoint(name string) bool {
	config, err := profile.LoadConfig()
	if err != nil {
		return false
	}
	entry, ok := config.Profiles[name]
	return ok && entry.Endpoint != nil
}
