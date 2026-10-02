package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
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

		// Version of the calling front end and GitHubRepo, as "<org>/<repo>", where its releases live.
		// Both are required: together they are the User-Agent, and they name the release to check against.
		Version    string
		GitHubRepo string
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

func newSessionFor(ctx context.Context, currentProfile *profile.Profile, opts ResolveSessionOptions) (Session, error) {
	org, repo, ok := strings.Cut(opts.GitHubRepo, "/")
	if !ok || org == "" || repo == "" {
		return Session{}, fmt.Errorf("GitHub repo '%s' is not of <org>/<repo> format", opts.GitHubRepo)
	}
	if opts.Version == "" {
		return Session{}, fmt.Errorf("no version given for GitHub repo '%s'", opts.GitHubRepo)
	}
	httpClient := http.NewClient(repo + "/" + opts.Version)
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
	c := client.New(ctx, endpoint, s.httpClient.UserAgent, s)
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
