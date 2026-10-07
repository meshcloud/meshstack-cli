package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Session struct {
	CurrentProfile *profile.Profile

	Credential Credential
	Client     func() (client.Client, error)
	MeshInfo   func() (client.MeshInfo, error)

	getWorkspace func() (meshstack.Workspace, error)
	httpClient   http.Client
}
type (
	SettingSources        = setting.Sources
	ResolveSessionOptions struct {
		SettingSources

		// UserAgent is required: it names the front end in every request, and the releases to check
		// against.
		http.UserAgent
	}
)

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	session, _, err := newSession(ctx, opts, false)
	if err != nil {
		return Session{}, err
	}
	resolved, err := session.resolveCredentials(ctx, opts)
	if err != nil {
		return Session{}, err
	}
	session.getWorkspace = sync.OnceValues(func() (meshstack.Workspace, error) {
		return opts.ResolveSetting(ctx, meshstack.WorkspaceSetting, session.CurrentProfile.WorkspaceSource())
	})
	return session.withCredential(ctx, resolved, opts)
}

// Scope names the profile, and the workspace where the settings choose it: only a browser login
// works in that workspace, while an API key works in the one that owns it.
func (s Session) Scope() string {
	scope := "profile " + string(s.CurrentProfile.Name)
	if _, browserLogin := s.Credential.Credential.(*credential.OidcLogin); browserLogin {
		if workspace, err := s.getWorkspace(); err == nil && workspace != meshstack.NoWorkspace {
			scope += " working in workspace " + string(workspace)
		}
	}
	return scope
}

func StoredSession(ctx context.Context, p *profile.Profile, opts ResolveSessionOptions) (Session, error) {
	session, err := newSessionFor(ctx, p, opts)
	if err != nil {
		return Session{}, err
	}
	stored, err := session.storedCredential(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	session.getWorkspace = func() (meshstack.Workspace, error) {
		return p.DefaultWorkspace, nil
	}
	return session.withCredential(ctx, stored, opts)
}

// newSession leaves the profiles locked with exclusiveLock only where it succeeds.
func newSession(ctx context.Context, opts ResolveSessionOptions, exclusiveLock bool) (_ Session, _ profile.Profiles, err error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		SettingSources: opts.SettingSources,
		ExclusiveLock:  exclusiveLock,
	})
	if err != nil {
		return Session{}, profile.Profiles{}, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, profiles.Unlock())
		}
	}()

	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting, currentProfile.EndpointSource())
	if err != nil {
		return Session{}, profile.Profiles{}, err
	} else if !endpoint.Equal(currentProfile.Endpoint) {
		// this prevents accidentally sending credentials to the wrong endpoint
		return Session{}, profile.Profiles{}, fmt.Errorf("profile '%s' is for endpoint '%s', not for endpoint '%s' of this run; "+
			"select a profile for that endpoint, or log in with a new profile name to create one", currentProfile, currentProfile.Endpoint, endpoint)
	}

	session, err := newSessionFor(ctx, currentProfile, opts)
	return session, profiles, err
}

// HttpClient is for a request that the meshStack client does not make, such as the download of the
// API docs. It carries the user agent of the front end, as the session's client does.
func (opts ResolveSessionOptions) HttpClient() (http.Client, error) {
	if err := opts.Validate(); err != nil {
		return http.Client{}, err
	}
	return http.NewClient(opts.UserAgent), nil
}

func newSessionFor(ctx context.Context, currentProfile *profile.Profile, opts ResolveSessionOptions) (Session, error) {
	httpClient, err := opts.HttpClient()
	if err != nil {
		return Session{}, err
	}
	return Session{
		CurrentProfile: currentProfile,
		httpClient:     httpClient,
		// Lazy, because OidcLogin needs /mesh/info before the authenticated client exists.
		MeshInfo: sync.OnceValues(func() (client.MeshInfo, error) {
			return getAndCheckMeshInfo(ctx, httpClient, currentProfile.Endpoint, opts.SettingSources)
		}),
		getWorkspace: func() (meshstack.Workspace, error) {
			return meshstack.NoWorkspace, nil
		},
	}, nil
}

func (s Session) withCredential(ctx context.Context, cred Credential, opts ResolveSessionOptions) (Session, error) {
	s.Credential = cred
	if err := s.Credential.Load(ctx); err != nil {
		return Session{}, err
	}
	s.Client = sync.OnceValues(func() (client.Client, error) {
		return s.buildClient(ctx, opts)
	})
	return s, nil
}

func getAndCheckMeshInfo(ctx context.Context, httpClient http.Client, endpoint xurl.URL, settingSources SettingSources) (client.MeshInfo, error) {
	meshInfo, err := client.NewMeshInfoClient(httpClient, endpoint).Read(ctx)
	if err != nil {
		return client.MeshInfo{}, err
	}
	if skipVersionCheck, err := settingSources.ResolveSetting(ctx, meshstack.SkipVersionCheckSetting); err != nil {
		return client.MeshInfo{}, err
	} else if skipVersionCheck {
		return meshInfo, nil
	}
	return meshInfo, meshInfo.CheckVersion()
}

func (s Session) buildClient(ctx context.Context, opts ResolveSessionOptions) (client.Client, error) {
	// Resolved here rather than while minting a token, which happens under the cache lock.
	// A session that names no workspace still builds a client: an api key or a manual token
	// needs none, and a credential that does need one says so when it mints.
	workspace, workspaceErr := s.getWorkspace()
	if workspaceErr != nil && !errors.Is(workspaceErr, setting.ErrNoSourceProvidedValue) {
		return client.Client{}, workspaceErr
	}
	endpoint := s.CurrentProfile.Endpoint
	slog.DebugContext(ctx, fmt.Sprintf("Building client for endpoint %s with user agent %s authenticated by %s, workspace %s",
		endpoint, s.httpClient.UserAgent, s.Credential.Name(), workspace))
	c := client.New(ctx, endpoint, s.httpClient, s)
	if skipVersionCheck, err := opts.ResolveSetting(ctx, meshstack.SkipVersionCheckSetting); err != nil {
		return client.Client{}, err
	} else if skipVersionCheck {
		// Skipping the check leaves resolution and this method without a single backend call,
		// so the Terraform provider does not block on an unreachable meshStack.
		return c, nil
	}
	if _, err := s.MeshInfo(); err != nil {
		return client.Client{}, err
	}
	if err := warnIfNewerReleasePresent(ctx, s.CurrentProfile.ConfigDir, s.httpClient, opts); err != nil {
		slog.WarnContext(ctx, "Cannot check for a newer release on GitHub: "+err.Error())
	}
	return c, nil
}

// ResolveWorkspace resolves the workspace that a session of opts works in, as ResolveSession does,
// and is NoWorkspace where no setting names one and the profile has no default workspace.
func ResolveWorkspace(ctx context.Context, opts ResolveSessionOptions) (meshstack.Workspace, error) {
	stored, _, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{SettingSources: opts.SettingSources, StoredOnly: true})
	var defaultWorkspace setting.Source
	switch {
	case err == nil:
		defaultWorkspace = stored.WorkspaceSource()
	case !errors.Is(err, profile.ErrNoStoredProfile):
		return meshstack.NoWorkspace, err
	}
	workspace, err := opts.ResolveSetting(ctx, meshstack.WorkspaceSetting, defaultWorkspace)
	if errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return meshstack.NoWorkspace, nil
	}
	return workspace, err
}
