package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// DevLocalProfile is reserved, so that a re-run of --dev-local overwrites it without asking.
// Keeping it off profile.DefaultName is what makes that safe: a developer's own `default`
// profile points at a real meshStack and must survive.
const DevLocalProfile = "dev-local"

// devLocalEndpoint is the only default endpoint anywhere in this package: guessing one for a
// real meshStack would send a credential somewhere nobody named, while guessing one for a
// stack on the developer's own machine is what makes the flag configless.
const devLocalEndpoint = "http://localhost:8080"

// ResolveForDevLocalLogin produces the session `meshstack login --dev-local` works through:
// ResolveSession with the two defaults that flag brings, and with the profile's own store,
// because bootstrapping a profile is the whole of what the flag does.
//
// The endpoint default is decided here rather than declared, because it sits below the
// selected profile's own endpoint while the profile name sits above currentProfile. Both
// would be wrong for every other command, so neither belongs on a declaration.
func ResolveForDevLocalLogin(ctx context.Context, settings setting.Source) (*Session, error) {
	defaults := devLocalDefaults{name: DevLocalProfile}
	selection, err := profile.Select(ctx, settings, setting.Environ(), defaults)
	if err != nil {
		return nil, err
	}
	if selection.Endpoint == "" && selection.Entry.Endpoint == nil {
		defaults.endpoint = devLocalEndpoint
	}
	store, err := profile.NewFileStore(selection.Name)
	if err != nil {
		return nil, err
	}
	return ResolveSession(ctx, ResolveSessionOptions{
		Settings:     settings,
		Defaults:     defaults,
		DemandMethod: credential.MethodApiKey,
		Store:        store,
	})
}

// devLocalDefaults answers those two settings and no others, unlike setting.Default, which
// answers whatever key it is asked because it is only ever placed in one setting's list.
type devLocalDefaults struct{ name, endpoint string }

func (d devLocalDefaults) Lookup(key string) (string, bool) {
	switch key {
	case profile.Name.EnvKey:
		return d.name, d.name != ""
	case meshstack.Endpoint.EnvKey:
		return d.endpoint, d.endpoint != ""
	}
	return "", false
}

func (devLocalDefaults) Describe(string) string { return "the --dev-local default" }

// LoginDevLocal bootstraps the profile from what a local dev stack publishes in /mesh/info,
// so running against one needs no .env file and no key issued by hand. It takes no
// LoginOptions: the exchange happens every time, and the workspace comes out of the same
// document.
func (s *Session) LoginDevLocal(ctx context.Context) (LoginResult, error) {
	return s.login(ctx, credential.MethodApiKey, func(result *LoginResult) error {
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
	if err := s.storeApiKey(ctx, &credential.ApiKey{
		Id:     dev.ApiKeyClientId,
		Secret: dev.ApiKeyClientSecret,
	}, token); err != nil {
		return err
	}
	result.Username = dev.ApiKeyClientId

	// The dev stack's key is not workspace-bound, so nothing here needs a workspace. The
	// profile does, for a workspace-scoped command run later.
	if s.Workspace == "" {
		s.Workspace = info.AdminWorkspaceIdentifier
	}
	if s.Workspace == "" {
		return nil
	}
	return s.rememberWorkspace(s.Workspace, result)
}
