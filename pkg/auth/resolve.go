package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// Session is one process's answer to "who am I, against what, in which workspace". It is the
// client.Authorization both front ends hand to client.New, so a 401 on a token it believed
// valid forces one re-mint.
//
// A session is safe for concurrent use: BearerToken is called before every request, and a
// Terraform provider makes many at once.
type Session struct {
	// Endpoint, Workspace and Profile are the resolved configuration. Profile is empty when
	// the credential did not come from a profile, which is the case a CI job and a building
	// block run land in.
	Endpoint  *url.URL
	Workspace workspace.Name
	Profile   string

	input Input
	store profile.Store

	// whole records that the credential arrived complete from above the profile layer — the
	// environment, or a Terraform provider block. Such a credential is never written into a
	// profile, and an API token among them is re-read from the front end rather than stored.
	whole bool

	// sources records where each value came from, for `meshstack profile view`.
	sources map[string]string

	mu sync.Mutex
	// current is the method every cached token belongs to. It is read under mu because
	// Login rewrites it.
	current method.Method
	// cached is the in-process cache: the token last obtained for this session's scope.
	// Checking it costs no I/O, which is what keeps BearerToken off the filesystem.
	//
	// A rejected token is not recorded beside it. RefreshBearerToken is told which token came
	// back 401 as an argument, so "do not hand this one out again" lasts exactly as long as the
	// call that needs it.
	cached profile.IssuedToken
	// oidcConfig is discovered at most once per process, and only by the login method.
	oidcConfig *oidc.ClientConfig
}

// Resolve produces the session an ordinary command works through. Every precedence rule
// applies here and only here.
//
// It ignores its context because resolution reads the two configuration files and nothing
// else. Every request it leads to — discovery, a refresh grant, the API key exchange — is
// made later, from BearerToken, and carries the context of the command that made it.
func Resolve(_ context.Context, in Input) (*Session, error) {
	return resolve(in, false)
}

// ResolveForLogin produces the session `meshstack auth login` works through. It differs in
// one way: the store is always the profile's, whatever supplied the credential, because
// writing a profile is that command's purpose. So MESHSTACK_API_SECRET reaches disk when
// `auth login --api-key` puts it there, and never otherwise.
func ResolveForLogin(_ context.Context, in Input) (*Session, error) {
	return resolve(in, true)
}

func resolve(in Input, forLogin bool) (*Session, error) {
	values := in.Explicit()
	sources := map[string]string{}

	endpointRaw, endpointFrom, endpointDetail := pick(values.Endpoint, envEndpoint)
	ws, wsFrom, wsDetail := pick(values.Workspace.String(), "")
	if ws == "" {
		if fromEnv := workspace.FromEnv(); fromEnv != "" {
			ws, wsFrom, wsDetail = fromEnv.String(), sourceEnv, "MESHSTACK_WORKSPACE"
		}
	}
	apiKeyId, _, _ := pick(values.ApiKey, envApiKey)

	config, err := profile.LoadConfig()
	if err != nil {
		return nil, err
	}

	session := &Session{input: in, sources: sources, Workspace: workspace.Name(ws)}

	// A credential is resolved as a whole, never field by field: take the highest-ranked
	// source that supplies a complete one.
	//
	// Two callers skip it. `auth login`, because its whole job is to write whatever it was
	// given into a profile. And a caller that named a profile through a flag or a provider
	// block attribute, because that is the top layer of the same precedence order — naming a
	// profile is a statement about which credential to use, and `meshstack auth status
	// --profile dev` reporting somebody's exported API key instead is the wart that proves it.
	// MESHSTACK_PROFILE does not count here: it sits in the environment layer, alongside
	// MESHSTACK_API_KEY, so neither outranks the other and the credential wins as before.
	if !forLogin && values.Profile == "" {
		if credential, ok := wholeCredential(values, apiKeyId); ok {
			// The endpoint and the workspace are their own axes of the precedence order, so a
			// profile may still supply them where the credential came from the environment.
			// Only a profile named explicitly, or the default one, is consulted: matching by
			// endpoint is impossible without an endpoint, and picking a profile because of its
			// credential would contradict the credential this command was given.
			if entry, name, found := plainProfile(config, values.Profile); found {
				if endpointRaw == "" {
					endpointRaw, endpointFrom, endpointDetail = entry.Endpoint, sourceProfile, "'"+name+"'"
				}
				if ws == "" && entry.DefaultWorkspace != "" {
					session.Workspace, wsFrom, wsDetail = entry.DefaultWorkspace, sourceProfile, "'"+name+"' default"
				}
			}
			if endpointRaw == "" {
				return nil, diags.Errorf("meshStack endpoint is not configured",
					"a credential was supplied but no endpoint. Set it with --endpoint, %s, or a profile.", envEndpoint)
			}
			session.Endpoint, err = parseEndpoint(endpointRaw)
			if err != nil {
				return nil, err
			}
			session.current = credential.method
			session.whole = true
			session.store = profile.NewMemoryStore(profile.Credentials{
				Version:       profile.Version,
				Endpoint:      session.Endpoint.String(),
				CurrentMethod: credential.method,
				Methods:       credential.methods,
			})
			sources["endpoint"] = endpointFrom.describe(endpointDetail)
			sources["workspace"] = wsFrom.describe(wsDetail)
			sources["credential"] = credential.from
			slog.Debug("resolved a credential without a profile",
				"method", session.current, "source", credential.from, "store", session.store.Describe())
			return session, nil
		}
	}

	// Otherwise the profile is what holds the credential.
	name, nameFrom, err := selectProfile(in, config, values.Profile, endpointRaw, forLogin)
	if err != nil {
		return nil, err
	}
	session.Profile = name
	sources["profile"] = nameFrom

	entry := config.Profiles[name]
	if endpointRaw == "" {
		endpointRaw, endpointFrom, endpointDetail = entry.Endpoint, sourceProfile, "'"+name+"'"
	}
	if endpointRaw == "" {
		return nil, diags.Errorf("meshStack endpoint is not configured",
			"profile %q has no endpoint. Set it with --endpoint, %s, or `meshstack profile set endpoint <url>`.", name, envEndpoint)
	}
	session.Endpoint, err = parseEndpoint(endpointRaw)
	if err != nil {
		return nil, err
	}

	if ws == "" && entry.DefaultWorkspace != "" {
		session.Workspace, wsFrom, wsDetail = entry.DefaultWorkspace, sourceProfile, "'"+name+"' default"
	}

	store, err := profile.NewFileStore(name)
	if err != nil {
		return nil, err
	}
	credentials, err := store.Read()
	if err != nil {
		return nil, err
	}

	// The endpoint check has to cover the whole file rather than each method, because the
	// common path uses a cached access token without consulting a method at all. Without it,
	// repointing a profile's endpoint would send a stored bearer token to a different
	// meshStack instance.
	if credentials.Endpoint != "" && !sameEndpoint(credentials.Endpoint, session.Endpoint.String()) {
		return nil, diags.Errorf("this credential belongs to a different meshStack",
			"profile %q was logged in to %s, but this command targets %s. Pick another profile with --profile, or log in again.",
			name, credentials.Endpoint, session.Endpoint)
	}

	session.store = store

	current, err := currentMethod(credentials, values.Method, forLogin)
	if err != nil {
		return nil, err
	}
	session.current = current

	sources["endpoint"] = endpointFrom.describe(endpointDetail)
	sources["workspace"] = wsFrom.describe(wsDetail)
	sources["credential"] = sourceProfile.describe("'" + name + "'")
	slog.Debug("resolved a credential from a profile",
		"profile", name, "method", current, "endpoint", session.Endpoint.String(), "workspace", session.Workspace)
	return session, nil
}

// plainProfile finds the profile a caller named, or the default one, without matching on the
// endpoint and without failing on a name that does not exist.
func plainProfile(config profile.Config, explicit string) (profile.Profile, string, bool) {
	name := explicit
	if name == "" {
		name = env(envProfile)
	}
	if name == "" {
		name = config.CurrentProfile
	}
	if name == "" {
		name = DefaultProfile
	}
	entry, ok := config.Profiles[name]
	return entry, name, ok
}

// wholeCredential reports a credential supplied entirely above the profile layer. Persisting
// one into the selected profile would let a CI job's identity overwrite a token minted from
// that profile's own method, and mix two identities in one file — so these get a memory
// store, and a building block run needs no files at all.
type resolvedCredential struct {
	method  method.Method
	methods profile.Methods
	from    string
}

func wholeCredential(values Values, apiKeyId string) (resolvedCredential, bool) {
	switch {
	case values.Method == method.Manual:
		return resolvedCredential{method: method.Manual, from: sourceExplicit.describe("api token")}, true
	case values.Method == "" && env(envApiToken) != "":
		return resolvedCredential{method: method.Manual, from: sourceEnv.describe(envApiToken)}, true
	case values.Method == method.ApiKey && apiKeyId != "":
		return resolvedCredential{
			method:  method.ApiKey,
			methods: profile.Methods{ApiKey: &profile.ApiKeyMethod{ClientId: apiKeyId}},
			from:    sourceExplicit.describe("api key"),
		}, true
	case values.Method == "" && apiKeyId != "" && env(envApiSecret) != "":
		return resolvedCredential{
			method:  method.ApiKey,
			methods: profile.Methods{ApiKey: &profile.ApiKeyMethod{ClientId: apiKeyId}},
			from:    sourceEnv.describe(envApiKey + " and " + envApiSecret),
		}, true
	}
	return resolvedCredential{}, false
}

// selectProfile applies the profile layer of the precedence order: an explicit name, then
// MESHSTACK_PROFILE, then a match on the endpoint, then the current profile, then "default".
func selectProfile(in Input, config profile.Config, explicit, endpoint string, forLogin bool) (name, from string, err error) {
	if named, fromSource, detail := pick(explicit, envProfile); named != "" {
		if _, ok := config.Profiles[named]; !ok && !forLogin {
			// A mistyped --profile must report an unknown profile rather than quietly
			// creating one. Creation happens in `auth login` and nowhere else.
			return "", "", diags.Errorf("unknown profile",
				"profile %q is not in %s. `meshstack auth login --profile %s` creates it.", named, describeConfigPath(), named)
		}
		return named, fromSource.describe(detail), nil
	}

	// An endpoint given on its own is almost always meant as "the instance I have a profile
	// for", so resolving it is friendlier than refusing.
	if endpoint != "" {
		var matches []string
		for candidate, entry := range config.Profiles {
			if sameEndpoint(entry.Endpoint, endpoint) {
				matches = append(matches, candidate)
			}
		}
		slices.Sort(matches)
		switch len(matches) {
		case 1:
			// A terraform plan whose identity depends on which profiles exist on the machine
			// should at least announce it.
			in.Warn(diags.Warnf("picked a profile by endpoint",
				"profile %q is the only one configured for %s, so this command uses its credentials. Name one with --profile to be explicit.",
				matches[0], endpoint))
			return matches[0], sourceProfile.describe("matched on the endpoint"), nil
		case 0:
			if forLogin {
				break
			}
			return "", "", diags.Errorf("no profile for this endpoint",
				"no profile in %s is configured for %s, and no credential was supplied through %s and %s. `meshstack auth login --endpoint %s` creates one.",
				describeConfigPath(), endpoint, envApiKey, envApiSecret, endpoint)
		default:
			return "", "", diags.Errorf("several profiles match this endpoint",
				"%s are all configured for %s. Pick one with --profile.", strings.Join(quoteAll(matches), ", "), endpoint)
		}
	}

	if config.CurrentProfile != "" {
		return config.CurrentProfile, sourceProfile.describe("currentProfile in " + describeConfigPath()), nil
	}
	return DefaultProfile, sourceDefault.describe("profile " + DefaultProfile), nil
}

// currentMethod decides which method mints for this session. A method other than the current
// one is chosen only when the profile has no current method at all — at first use, or after
// `meshstack auth logout`.
func currentMethod(credentials profile.Credentials, demanded method.Method, forLogin bool) (method.Method, error) {
	if demanded != "" {
		if !forLogin && credentials.CurrentMethod != "" && demanded != credentials.CurrentMethod {
			return "", diags.Errorf("this profile uses a different authentication method",
				"the profile's current method is %s. `meshstack auth login` is the only command that switches it.",
				credentials.CurrentMethod.Description())
		}
		return demanded, nil
	}
	if credentials.CurrentMethod != "" {
		return credentials.CurrentMethod, nil
	}
	switch {
	case credentials.Methods.Login != nil:
		slog.Info("this profile has no current method, using its browser login")
		return method.Login, nil
	case credentials.Methods.ApiKey != nil:
		slog.Info("this profile has no current method, using its API key")
		return method.ApiKey, nil
	}
	// Nothing stored yet. Login is the method `meshstack login` creates, and the error a
	// command gets from a profile with no credentials names it.
	return method.Login, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, diags.Errorf("the meshStack endpoint is not a valid URL", "%q: %v", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, diags.Errorf("the meshStack endpoint is not a valid URL",
			"%q has no scheme or no host. It should read like https://api.example.meshcloud.io.", raw)
	}
	return parsed, nil
}

func describeConfigPath() string {
	path, err := profile.ConfigPath()
	if err != nil {
		return "the meshStack CLI configuration"
	}
	return path
}

func quoteAll(values []string) []string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return quoted
}
