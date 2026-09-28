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

	Credential profile.CachedCredential
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

func newSession(ctx context.Context, opts ResolveSessionOptions) (Session, StoreFunc, error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		SettingSources: opts.SettingSources,
	})
	if err != nil {
		return Session{}, nil, err
	}

	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting, currentProfile.EndpointSource())
	if err != nil {
		return Session{}, nil, err
	} else if !endpoint.Equal(currentProfile.Endpoint) {
		// this prevents accidentally sending credentials to the wrong endpoint
		return Session{}, nil, fmt.Errorf("endpoint from profile '%s' does not match endpoint '%s' configured for session", currentProfile.Endpoint, endpoint)
	}

	buildHttpClient := func() (http.Client, error) {
		org, repo, ok := strings.Cut(opts.GitHubRepo, "/")
		if !ok || org == "" || repo == "" {
			return http.Client{}, fmt.Errorf("GitHub repo '%s' is not of <org>/<repo> format", opts.GitHubRepo)
		}
		if opts.Version == "" {
			return http.Client{}, fmt.Errorf("no version given for GitHub repo '%s'", opts.GitHubRepo)
		}
		return http.NewClient(repo + "/" + opts.Version), nil
	}

	httpClient, err := buildHttpClient()
	if err != nil {
		return Session{}, nil, err
	}

	return Session{
		CurrentProfile: currentProfile,
		httpClient:     httpClient,
		// Lazy, because OidcLogin needs /mesh/info before the authenticated client exists.
		MeshInfo: sync.OnceValues(func() (client.MeshInfo, error) {
			return getAndCheckMeshInfo(ctx, httpClient, endpoint, opts.SettingSources)
		}),
		getWorkspace: func() (meshstack.Workspace, error) {
			return meshstack.NoWorkspace, nil
		},
	}, profiles.Store, nil
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	session, _, err := newSession(ctx, opts)
	if err != nil {
		return Session{}, err
	}

	resolvedCredential, err := session.resolveCredentials(ctx, opts)
	if err != nil {
		return Session{}, err
	}
	session.Credential = session.CurrentProfile.CacheFor(resolvedCredential)
	if err := session.Credential.Load(ctx); err != nil {
		return Session{}, err
	}
	session.Client = sync.OnceValues(func() (client.Client, error) {
		return session.buildClient(ctx, opts)
	})
	session.getWorkspace = sync.OnceValues(func() (meshstack.Workspace, error) {
		return opts.ResolveSetting(ctx, meshstack.WorkspaceSetting, session.CurrentProfile.WorkspaceSource())
	})
	return session, nil
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
