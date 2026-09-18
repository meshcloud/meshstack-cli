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
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Session struct {
	ConfigDir   config.Directory
	Credentials profile.Credentials
	Credential  credential.Credential
	Endpoint    xurl.URL
	Client      func() (client.Client, error)
	Store       func(ctx context.Context) error

	getWorkspace    func() (meshstack.Workspace, error)
	httpClient      http.Client
	checkedMeshInfo func() (client.MeshInfo, error)
}
type (
	SettingSources        = setting.Sources
	ResolveSessionOptions struct {
		SettingSources

		// Version of the calling front end and GitHubRepo, as "<org>/<repo>", where its releases live.
		// Both are required: together they are the User-Agent, and they name the release to check against.
		Version    string
		GitHubRepo string

		ForceAuthWith credential.Name
	}
)

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		SettingSources: opts.SettingSources,
	})
	if err != nil {
		return Session{}, err
	}

	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting, currentProfile.EndpointSource())
	if err != nil {
		return Session{}, err
	} else if currentProfile.Endpoint != nil && !endpoint.Equal(*currentProfile.Endpoint) {
		// this prevents accidentally sending credentials to the wrong endpoint
		return Session{}, fmt.Errorf("endpoint from profile '%s' does not match endpoint '%s' configured for session", currentProfile.Endpoint, endpoint)
	}

	userAgent, err := opts.userAgent()
	if err != nil {
		return Session{}, err
	}
	httpClient := http.NewClient(userAgent)

	session := Session{
		ConfigDir:  currentProfile.ConfigDir,
		Endpoint:   endpoint,
		httpClient: httpClient,
		// OidcLogin needs /mesh/info rather early, so provide it lazily
		// without having the full session-authenticated client.Client constructed (see below)
		// also check for version match if not skipped by setting.
		checkedMeshInfo: sync.OnceValues(func() (client.MeshInfo, error) {
			return getAndCheckMeshInfo(ctx, httpClient, endpoint, opts.SettingSources)
		}),
	}

	session.Credentials, session.Credential, err = session.resolveCredentials(ctx, currentProfile, opts)
	if err != nil {
		return Session{}, err
	}
	session.Client = sync.OnceValues(func() (client.Client, error) {
		return session.buildClient(ctx, opts)
	})
	session.getWorkspace = sync.OnceValues(func() (meshstack.Workspace, error) {
		return session.resolveWorkspace(ctx, *currentProfile, opts)
	})
	session.Store = func(ctx context.Context) error {
		// start with storing the precious credentials,
		// store as much as possible no matter if sth fails
		errs := []error{session.Credentials.Store(ctx)}

		switch defaultWorkspace, err := session.getWorkspace(); {
		case err == nil:
			currentProfile.DefaultWorkspace = defaultWorkspace
		case !errors.Is(err, setting.ErrNoSourceProvidedValue):
			// A session that resolved no workspace at all stores none, which is not a failure.
			errs = append(errs, err)
		}

		// Note: currentProfile is a pointer and will update the profile in the Profiles.Profiles map,
		// see profile.ResolveProfile above, so this Store saves the DefaultWorkspace.
		return errors.Join(append(errs, profiles.Store(ctx))...)
	}
	return session, nil
}

func (o ResolveSessionOptions) userAgent() (string, error) {
	org, repo, ok := strings.Cut(o.GitHubRepo, "/")
	if !ok || org == "" || repo == "" {
		return "", fmt.Errorf("GitHub repo '%s' is not of <org>/<repo> format", o.GitHubRepo)
	}
	if o.Version == "" {
		return "", fmt.Errorf("no version given for GitHub repo '%s'", o.GitHubRepo)
	}
	return repo + "/" + o.Version, nil
}

func (s Session) MeshInfo() (client.MeshInfo, error) {
	// cached supplier (held internally)
	return s.checkedMeshInfo()
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
	// Eagerly resolves workspace to make later credentials accessing s.Workspace() not hold the cache lock unnecessarily long.
	// A session that names no workspace still builds a client, as an api key or a manual token needs none;
	// the credentials that do need one say so when they mint a token.
	workspace, workspaceErr := s.getWorkspace()
	if workspaceErr != nil && !errors.Is(workspaceErr, setting.ErrNoSourceProvidedValue) {
		return client.Client{}, workspaceErr
	}
	slog.DebugContext(ctx, fmt.Sprintf("Building client for endpoint %s with user agent %s authenticated by %T, workspace %s",
		s.Endpoint, s.httpClient.UserAgent, s.Credential, workspace))
	c := client.New(ctx, s.Endpoint, s.httpClient.UserAgent, s)
	if skipVersionCheck, err := opts.ResolveSetting(ctx, meshstack.SkipVersionCheckSetting); err != nil {
		return client.Client{}, err
	} else if skipVersionCheck {
		// Neither ResolveSession nor this method does any HTTP backend call if version check is skipped
		// which is important for Terraform provider behavior not blocking early on when backend is unreachable.
		return c, nil
	}
	// this checks the meshStack backend version
	if _, err := s.checkedMeshInfo(); err != nil {
		return client.Client{}, err
	}
	if err := warnIfNewerReleasePresent(ctx, s.ConfigDir, s.httpClient, opts); err != nil {
		slog.WarnContext(ctx, "Cannot check for a newer release on GitHub: "+err.Error())
	}
	return c, nil
}

func (s Session) resolveWorkspace(ctx context.Context, currentProfile profile.Profile, opts ResolveSessionOptions) (meshstack.Workspace, error) {
	// Sources resolving the workspace are usually interested in the available list of workspaces,
	// so provide a context able to lazily fetch them during resolution.
	// Note that a session-authenticated Session.Client is build also lazily when fetching workspaces,
	// which is possibly now thanks to Session.Credential(s) being set/resolved before.
	// A deadlock is avoided by listing through a session that names no workspace, see withNoWorkspace.
	ctxWithWorkspaces := meshstack.SetWorkspacesInContext(ctx, sync.OnceValues(func() ([]client.MeshWorkspace, error) {
		c, err := s.withNoWorkspace(ctx, opts).Client()
		if err != nil {
			return nil, err
		}
		return c.Workspace.List(ctx)
	}))
	return opts.ResolveSetting(ctxWithWorkspaces, meshstack.WorkspaceSetting, currentProfile.WorkspaceSource())
}

// withNoWorkspace is the session the workspace resolution itself can use: it names no workspace, so
// nothing done through it can ask back for the workspace being resolved. It needs a Client of its
// own, because Session.Client is memoized around the session it was built for and would hand back
// one authorized by that very workspace.
func (s Session) withNoWorkspace(ctx context.Context, opts ResolveSessionOptions) Session {
	unscoped := s
	unscoped.getWorkspace = func() (meshstack.Workspace, error) {
		return meshstack.NoWorkspace, nil
	}
	unscoped.Client = sync.OnceValues(func() (client.Client, error) {
		return unscoped.buildClient(ctx, opts)
	})
	return unscoped
}
