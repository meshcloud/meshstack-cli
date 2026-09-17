package auth

import (
	"context"
	"errors"
	"fmt"
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
	ConfigDir       config.Directory
	Credentials     profile.Credentials
	Credential      credential.Credential
	Endpoint        xurl.URL
	Workspace       meshstack.Workspace
	HttpClient      http.Client
	Store           func(ctx context.Context) error
	CheckedMeshInfo func() (client.MeshInfo, error)
}

type ResolveSessionOptions struct {
	setting.ExplicitSourcesOption

	// Version of the calling front end and GitHubRepo, as "<org>/<repo>", where its releases live.
	// Both are required: together they are the User-Agent, and they name the release to check against.
	Version    string
	GitHubRepo string

	ForceAuthWith credential.Name
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		ExplicitSourcesOption: opts.ExplicitSourcesOption,
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

	workspace, err := opts.ResolveSetting(ctx, meshstack.WorkspaceSetting, currentProfile.WorkspaceSource())
	if err != nil && !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return Session{}, err
	}

	userAgent, err := opts.userAgent()
	if err != nil {
		return Session{}, err
	}
	httpClient := http.NewClient(userAgent)

	session := Session{
		ConfigDir:  currentProfile.ConfigDir,
		Endpoint:   endpoint,
		Workspace:  workspace,
		HttpClient: httpClient,
		// OidcLogin needs /mesh/info rather early, so provide it lazily (and checked for version if not skipped)
		CheckedMeshInfo: sync.OnceValues(func() (client.MeshInfo, error) {
			return getAndCheckMeshInfo(ctx, httpClient, endpoint, opts.ExplicitSourcesOption)
		}),
	}

	if creds, current, err := session.resolveCredentials(ctx, currentProfile, opts); err != nil {
		return Session{}, err
	} else {
		session.Credentials = creds
		session.Credential = current
		session.Store = func(ctx context.Context) error {
			// store them all, no matter if both error or only one of them
			return errors.Join(profiles.Store(ctx), creds.Store(ctx))
		}
		return session, nil
	}
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

func getAndCheckMeshInfo(ctx context.Context, httpClient http.Client, endpoint xurl.URL, opts setting.ExplicitSourcesOption) (client.MeshInfo, error) {
	meshInfo, err := client.NewMeshInfoClient(httpClient, endpoint).Read(ctx)
	if err != nil {
		return client.MeshInfo{}, err
	}
	if skipVersionCheck, err := opts.ResolveSetting(ctx, meshstack.SkipVersionCheckSetting); err != nil {
		return client.MeshInfo{}, err
	} else if skipVersionCheck {
		return meshInfo, nil
	}
	return meshInfo, meshInfo.CheckVersion()
}
