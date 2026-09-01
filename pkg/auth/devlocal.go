package auth

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// DevLocalProfile is the name --dev-local resolves through, and the prefix every profile it
// writes carries. The prefix is reserved, so that a re-run overwrites those profiles without
// asking. Keeping it off profile.DefaultName is what makes that safe: a developer's own
// `default` profile points at a real meshStack and must survive.
const DevLocalProfile = "dev-local"

// DevLocalProfileName turns an api key's configured name or a login's username into the profile
// --dev-local writes it to: dev-local-terraform-provider-acceptance,
// dev-local-partner-at-meshcloud-io.
//
// An `@` becomes `-at-` rather than a plain `-`, so that the two halves of an address stay
// readable. Everything else outside a-z0-9 becomes a single `-`, which can in principle map two
// different names onto one profile; that fails here rather than letting the later one silently
// take the earlier one's credential.
func DevLocalProfileName(name string) (string, error) {
	slug := devLocalSlug(name)
	if slug == "" {
		return "", diags.Errorf("cannot name a profile after this local dev credential",
			"%q has no character a profile name can be built from.", name)
	}
	claimed, taken := devLocalSlugs[slug]
	if taken && claimed != name {
		return "", diags.Errorf("two local dev credentials want the same profile",
			"%q and %q both become the profile %s%s. Rename one of them in the dev stack's configuration.",
			claimed, name, DevLocalProfile+"-", slug)
	}
	devLocalSlugs[slug] = name
	return DevLocalProfile + "-" + slug, nil
}

// devLocalSlugs remembers what each slug was built from, for the collision check above. One login
// runs one --dev-local, so a process-wide map is the whole of the bookkeeping needed.
var devLocalSlugs = map[string]string{}

func devLocalSlug(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.ReplaceAll(name, "@", "-at-")) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

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
	if len(dev.ApiKeys) == 0 {
		return diags.Errorf("this meshStack publishes no local dev api keys",
			"%s serves devLocalCredentials in /mesh/info but no apiKeys in it, so there is nothing to log in with.",
			s.Endpoint)
	}

	names, err := s.bootstrapApiKeyProfiles(ctx, dev)
	if err != nil {
		return err
	}
	userNames, err := s.bootstrapUserProfiles(dev)
	if err != nil {
		return err
	}
	result.Username = strings.Join(append(names, userNames...), ", ")
	return nil
}

// bootstrapApiKeyProfiles writes one logged-in profile per published key. Every key gets one
// rather than one being chosen here, because they do not hold the same rights and which is
// wanted is the caller's question, not this flag's.
func (s *Session) bootstrapApiKeyProfiles(ctx context.Context, dev *client.DevLocalCredentials) ([]string, error) {
	written := make([]string, 0, len(dev.ApiKeys))
	for _, name := range slices.Sorted(maps.Keys(dev.ApiKeys)) {
		key := dev.ApiKeys[name]
		profileName, err := DevLocalProfileName(name)
		if err != nil {
			return nil, err
		}
		token, err := apiLogin(ctx, s.Endpoint, key.ClientId, key.ClientSecret)
		if err != nil {
			return nil, err
		}
		if err := profile.Ensure(profileName, &s.Endpoint); err != nil {
			return nil, err
		}
		store, err := profile.NewFileStore(profileName)
		if err != nil {
			return nil, err
		}
		if _, err := store.Update(ctx, func(c profile.Credentials) (profile.Credentials, error) {
			c.Version = profile.Version
			c.Endpoint = &s.Endpoint
			stored := credential.ApiKey{Id: key.ClientId, Secret: key.ClientSecret, AccessToken: issuedToken(token)}
			c.Credential = switchTo(c.Credential, credential.FromApiKey(stored))
			return c, nil
		}); err != nil {
			return nil, err
		}
		written = append(written, profileName)
	}
	return written, nil
}

// bootstrapUserProfiles writes one profile per seeded login, carrying the endpoint and nothing
// else. No credential, because the CLI's keycloak client runs the authorization code flow and has
// no password grant, so `meshstack login --profile <name>` still has to do the browser exchange.
//
// No default workspace either, deliberately: one of these logs in and discovers what it can reach
// exactly as any other user does, and a workspace put here by the flag would make that path
// untested for the one stack where it is easiest to test. What the profile saves is naming the
// endpoint again, and nothing more.
func (s *Session) bootstrapUserProfiles(dev *client.DevLocalCredentials) ([]string, error) {
	written := make([]string, 0, len(dev.Users))
	for _, username := range slices.Sorted(maps.Keys(dev.Users)) {
		profileName, err := DevLocalProfileName(username)
		if err != nil {
			return nil, err
		}
		if err := profile.Ensure(profileName, &s.Endpoint); err != nil {
			return nil, err
		}
		written = append(written, profileName)
	}
	return written, nil
}
