package auth

import (
	"context"
	"errors"
	"fmt"

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

	session := Session{Endpoint: endpoint, HttpClient: http.NewClient(opts.UserAgent)}

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
	Credentials profile.Credentials
	Credential  credential.Credential
	Endpoint    xurl.URL
	HttpClient  http.Client
	Store       func(ctx context.Context) error
}
