package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type ResolveSessionOptions struct {
	setting.ExplicitSourcesOption

	ForceAuthWith credential.Name
	UserAgent     string
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	currentProfile, profiles, err := profile.ResolveProfile(ctx, profile.ResolveProfileOptions{
		ExplicitSourcesOption: opts.ExplicitSourcesOption,
	})
	if err != nil {
		return Session{}, err
	}

	endpoint, err := opts.ResolveSetting(meshstack.EndpointSetting, currentProfile.EndpointSource())
	if err != nil {
		return Session{}, err
	} else if currentProfile.Endpoint != nil && !endpoint.Equal(*currentProfile.Endpoint) {
		// this prevents accidentally sending credentials to the wrong endpoint
		return Session{}, fmt.Errorf("endpoint from profile '%s' does not match endpoint '%s' configured for session", currentProfile.Endpoint, endpoint)
	}

	workspace, err := opts.ResolveSetting(meshstack.WorkspaceSetting, currentProfile.WorkspaceSource())
	if err != nil && !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return Session{}, err
	}

	httpClient := http.NewClient(opts.UserAgent)

	session := Session{
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

type Session struct {
	Credentials     profile.Credentials
	Credential      credential.Credential
	Endpoint        xurl.URL
	Workspace       meshstack.Workspace
	HttpClient      http.Client
	Store           func(ctx context.Context) error
	CheckedMeshInfo func() (client.MeshInfo, error)
}

// WithWorkspace is a session acting in another workspace. The copy shares Credentials, so one
// refresh token and one file lock serve every workspace a run touches.
func (s Session) WithWorkspace(workspace meshstack.Workspace) Session {
	inWorkspace := s
	inWorkspace.Workspace = workspace
	return inWorkspace
}

func getAndCheckMeshInfo(ctx context.Context, httpClient http.Client, endpoint xurl.URL, opts setting.ExplicitSourcesOption) (client.MeshInfo, error) {
	meshInfo, err := client.NewMeshInfoClient(httpClient, endpoint).Read(ctx)
	if err != nil {
		return client.MeshInfo{}, err
	}
	if skipVersionCheck, err := opts.ResolveSetting(meshstack.SkipVersionCheckSetting); err != nil {
		return client.MeshInfo{}, err
	} else if skipVersionCheck {
		return meshInfo, nil
	}
	return meshInfo, meshInfo.CheckVersion()
}
