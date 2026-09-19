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

		// CreateProfileIfMissing writes the profile this run names as a new one instead of failing
		// on it. A login sets it, and no other command does: an unknown name is a typo there.
		CreateProfileIfMissing bool
	}
)

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		SettingSources:         opts.SettingSources,
		CreateProfileIfMissing: opts.CreateProfileIfMissing,
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
		// Lazy, because OidcLogin needs /mesh/info before the authenticated client exists.
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
		// The credentials go first, and every step runs even after an earlier one failed.
		errs := []error{session.Credentials.Store(ctx)}

		switch defaultWorkspace, err := session.getWorkspace(); {
		case err == nil:
			currentProfile.DefaultWorkspace = defaultWorkspace
		case !errors.Is(err, setting.ErrNoSourceProvidedValue):
			errs = append(errs, err)
		}

		// currentProfile points into the map profiles holds, so the assignment above is what
		// profiles.Store writes out.
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
	// Resolved here rather than while minting a token, which happens under the cache lock.
	// A session that names no workspace still builds a client: an api key or a manual token
	// needs none, and a credential that does need one says so when it mints.
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
		// Skipping the check leaves resolution and this method without a single backend call,
		// so the Terraform provider does not block on an unreachable meshStack.
		return c, nil
	}
	if _, err := s.checkedMeshInfo(); err != nil {
		return client.Client{}, err
	}
	if err := warnIfNewerReleasePresent(ctx, s.ConfigDir, s.httpClient, opts); err != nil {
		slog.WarnContext(ctx, "Cannot check for a newer release on GitHub: "+err.Error())
	}
	return c, nil
}

func (s Session) resolveWorkspace(ctx context.Context, currentProfile profile.Profile, opts ResolveSessionOptions) (meshstack.Workspace, error) {
	// A source that resolves the workspace usually wants the list to pick from, so the context
	// carries a lazy fetch of it. That fetch lists through a session naming no workspace, which
	// is what keeps it from asking back for the workspace being resolved; see withNoWorkspace.
	ctxWithWorkspaces := meshstack.SetWorkspacesInContext(ctx, sync.OnceValues(func() (r meshstack.Workspaces, err error) {
		var c client.Client
		c, err = s.withNoWorkspace(ctx, opts).Client()
		if err != nil {
			return r, err
		}
		r.ProfileDefaultWorkspace = currentProfile.DefaultWorkspace
		r.Items, err = c.Workspace.List(ctx)
		if httpError, ok := errors.AsType[http.Error](err); ok && httpError.IsForbidden() {
			err = fmt.Errorf("cannot list workspaces; try logging into meshPanel UI first, got: %w", httpError)
		} else if err == nil && len(r.Items) == 0 {
			err = errors.New("no workspaces found; try logging into meshPanel UI first and/or become member of a workspace")
		}
		return
	}))
	ctxWithCredential := credential.SetNameInContext(ctxWithWorkspaces, s.Credentials.NameOf(s.Credential))
	return opts.ResolveSetting(ctxWithCredential, meshstack.WorkspaceSetting, currentProfile.WorkspaceSource())
}

// withNoWorkspace is the session the workspace resolution itself can use: it names no workspace, so
// nothing done through it can ask back for the workspace being resolved. It needs a Client of its
// own, because Session.Client is memoized and would hand back one authorized by that very workspace.
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
