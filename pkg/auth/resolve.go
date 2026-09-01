package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// The three failures `meshstack auth login` may answer with a prompt, and the reason
// ResolveSession names them at all: every other command reports the error as it came,
// because a command that is not a login should not open a login dialogue.
var (
	ErrNoEndpoint  = errors.New("no meshStack endpoint")
	ErrNoApiSecret = errors.New("no API key secret")
	ErrNoApiToken  = errors.New("no meshStack API token")
)

// Session is the client.Authorization both front ends hand to client.New. It is safe for
// concurrent use, because a Terraform provider has many requests in flight at once.
type Session struct {
	Endpoint  xurl.URL
	Workspace string
	Profile   string

	// noInput says nobody is here to wait on. It is the resolved tty.NoInput setting rather
	// than a question asked of the terminal, because a browser login reaches a person
	// through stderr from a pipe as well.
	noInput bool

	// resolved is the credential the ranked order settled on. The store holds it too for
	// every command but a login, where the store is the file the login is about to write.
	resolved credential.Credential

	origins []setting.Origin
	store   profile.Store

	mu sync.Mutex
	// current is the method every cached token belongs to. It is read under mu because
	// Login rewrites it.
	current credential.Method
	// cached costs no I/O to check, which is what keeps BearerToken off the filesystem. A
	// rejected token is not recorded beside it: RefreshBearerToken takes that as an argument,
	// so "do not hand this one out again" lasts exactly as long as the call that needs it.
	cached credential.IssuedToken
	// oidcConfig is discovered at most once per process, and only by the login method.
	oidcConfig *oidc.Client
}

// ResolveSessionOptions is a struct rather than bare arguments so that a later addition — a
// clock, a second source — is a new field instead of a signature change both front ends have
// to follow.
type ResolveSessionOptions struct {
	// Settings is the front end's own source: the CLI's flags, or the provider block. Nil is
	// a front end contributing nothing explicit rather than an error.
	Settings setting.Source

	// DemandMethod is not a setting: it has no MESHSTACK_* name and supplies no value. It
	// filters what every source may offer, so a source carrying another method's identity is
	// passed over as if it were empty — which is what keeps a bare `meshstack login` from
	// resolving an exported API key and refusing to open a browser.
	DemandMethod credential.Method

	// Store is handed in by a command that configures a profile, and saying so has two
	// effects: the credential reaches that file whatever supplied it, and a profile that
	// does not exist yet is tolerated rather than reported.
	//
	// Left nil, the store is chosen: the profile's credentials file when the credential came
	// from it, and a memory store when it came from anywhere above. That is what makes a CI
	// job and a building block run need no files at all.
	Store profile.Store

	// Defaults sits below the environment and above the selected profile. Only `--dev-local`
	// brings any, and it decides its endpoint default only after finding that the profile
	// names none, because both of its defaults would be wrong for every other command.
	Defaults setting.Source
}

// ResolveSession applies every precedence rule, here and only here, in the order
// explicit → environment → profile → built-in default.
//
// It makes no request: it reads the two configuration files and nothing else. The context is
// for the log records and for the warnings the credential walk emits.
func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (*Session, error) {
	sources := []setting.Source{opts.Settings, setting.Environ(), opts.Defaults}

	selection, err := profile.Select(ctx, sources...)
	if err != nil {
		return nil, err
	}
	// A mistyped --profile must report an unknown profile rather than quietly creating one.
	if selection.Named && !selection.Exists && opts.Store == nil {
		return nil, diags.Errorf("unknown profile",
			"profile %q is not in %s. `meshstack auth login --profile %s` creates it.",
			selection.Name, profile.DescribeConfigPath(), selection.Name)
	}

	// The endpoint's list is explicit → environment → profile throughout. Select evaluated
	// the prefix, because the profile is what it was being used to pick; this is the one
	// remaining source, and re-reading the prefix could not change the answer.
	session := &Session{Profile: selection.Name, origins: selection.Origins}
	raw := selection.Endpoint
	if raw == "" && selection.Entry.Endpoint != nil {
		raw = selection.Entry.Endpoint.String()
		session.origins = append(session.origins, setting.Origin{
			Key: meshstack.Endpoint.EnvKey, Source: "profile " + selection.Name,
		})
	}
	if raw == "" {
		return nil, diags.Wrap(ErrNoEndpoint, "meshStack endpoint is not configured",
			"profile %q names no endpoint. Set it with %s, or with `meshstack profile set endpoint <url>`.",
			selection.Name, meshstack.Endpoint.EnvKey)
	}
	if session.Endpoint, err = meshstack.ParseEndpoint(raw); err != nil {
		return nil, err
	}

	resolved, err := resolveCredential(ctx, opts, selection, session.Endpoint, sources)
	if err != nil {
		return nil, err
	}
	session.resolved, session.current = resolved.credential, resolved.credential.Current
	session.origins = append(session.origins, resolved.origins...)

	// Reported here rather than from Select, because it is the credential that makes an
	// unconfigured machine a failure: one supplied from above needs no profile at all.
	if !selection.Exists && resolved.fromProfile && opts.Store == nil {
		return nil, diags.Errorf("no profile for this endpoint",
			"no profile in %s is configured for %s, and no credential was supplied through %s and %s. `meshstack auth login --endpoint %s` creates one.",
			profile.DescribeConfigPath(), session.Endpoint, credential.ApiKeyId.EnvKey, credential.ApiSecret.EnvKey, session.Endpoint)
	}

	workspace, from, err := setting.Resolve(meshstack.Workspace, sources...)
	if err != nil {
		return nil, err
	}
	switch {
	case from != nil:
		session.Workspace = workspace
		session.origins = append(session.origins, setting.Origin{
			Key: meshstack.Workspace.EnvKey, Source: from.Describe(meshstack.Workspace.EnvKey),
		})
	case selection.Entry.DefaultWorkspace != "":
		session.Workspace = selection.Entry.DefaultWorkspace
		session.origins = append(session.origins, setting.Origin{
			Key: meshstack.Workspace.EnvKey, Source: "profile " + selection.Name,
		})
	}

	if session.noInput, from, err = setting.Resolve(tty.NoInput, sources...); err != nil {
		return nil, err
	}
	if from != nil {
		session.origins = append(session.origins, setting.Origin{
			Key: tty.NoInput.EnvKey, Source: from.Describe(tty.NoInput.EnvKey),
		})
	}

	switch {
	case opts.Store != nil:
		session.store = opts.Store
	case resolved.fromProfile:
		if session.store, err = profile.NewFileStore(selection.Name); err != nil {
			return nil, err
		}
	default:
		// An ephemeral credential — from HCL, the environment or a prompt — lands in a store
		// that cannot write, so it can never mix its identity into somebody's profile.
		session.store = profile.NewMemoryStore(profile.Credentials{
			Endpoint: &session.Endpoint, Credential: resolved.credential,
		})
	}

	slog.DebugContext(ctx, "resolved a session",
		"profile", session.Profile, "method", session.current,
		"endpoint", session.Endpoint.String(), "workspace", session.Workspace,
		"store", session.store.Describe())
	return session, nil
}

// Origins is where each resolved value came from, in the order the resolution walked them.
// A slice rather than a map: there is one writer, and that order is the one a person reading
// `meshstack profile view` wants.
func (s *Session) Origins() []setting.Origin { return s.origins }
