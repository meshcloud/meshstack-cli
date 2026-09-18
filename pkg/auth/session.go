package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

type (
	// ResolveSessionOptions carries the setting sources, the calling front end and the credential to insist on.
	ResolveSessionOptions = auth.ResolveSessionOptions
	// Session is one resolved profile and credential, and builds an authorized client from them.
	Session struct {
		internal auth.Session
	}
	// Status is what the meshStack backend reports about itself, and the endpoint it was asked at.
	Status struct {
		client.MeshInfo

		Endpoint xurl.URL
	}
)

// ResolveSession resolves the profile, creating a default one when none exists. It checks no
// backend version, so prefer ResolveClient where the Session itself is not needed.
func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (result Session, err error) {
	result.internal, err = auth.ResolveSession(ctx, opts)
	return
}

// Client builds the authorized client and checks the backend version, unless MESHSTACK_SKIP_VERSION_CHECK
// asks it not to. It is built once, from the context ResolveSession was given, so it takes none of its own.
func (s Session) Client() (client.Client, error) {
	return s.internal.Client()
}

// ResolveClient resolves a session and builds its client in one call.
func ResolveClient(ctx context.Context, opts ResolveSessionOptions) (client.Client, error) {
	session, err := ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client()
}

// Status reads the backend's info and mints a bearer token, so that an unusable credential fails
// here. A [Session.Store] afterwards persists that token.
func (s Session) Status(ctx context.Context) (status Status, err error) {
	status.Endpoint = s.internal.Endpoint
	status.MeshInfo, err = s.internal.MeshInfo()
	if err != nil {
		return
	}
	_, err = s.internal.GetBearerToken(ctx)
	return
}

// Store writes the resolved session, its credentials and its cached tokens into the current profile.
func (s Session) Store(ctx context.Context) error {
	return s.internal.Store(ctx)
}
