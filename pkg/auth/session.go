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
	// Session is a facade for auth.Session obtained by ResolveSession, exposing mainly an authorized Session.Client.
	// Consider using ResolveClient instead.
	Session struct {
		internal auth.Session
	}
	// Status is returned by Session.Status().
	Status struct {
		client.MeshInfo

		Endpoint xurl.URL
	}
)

// ResolveSession resolves the profile (creating a default one if non exists) and a Session from it.
// It does not check the backend version, so prefer ResolveClient where the Session is not needed.
func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (result Session, err error) {
	result.internal, err = auth.ResolveSession(ctx, opts)
	return
}

// Client builds the client and checks the backend version, if not skipped using [meshstack.SkipVersionCheckSetting].
// It is built once, from the context ResolveSession was given, so it takes none of its own.
func (s Session) Client() (client.Client, error) {
	return s.internal.Client()
}

// ResolveClient resolves a session and builds its client, authorized against the meshStack backend.
// Convenient wrapper for ResolveSession(...) -> Session.Client(...).
func ResolveClient(ctx context.Context, opts ResolveSessionOptions) (client.Client, error) {
	session, err := ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client()
}

// Status retrieves info from backend and also mints a bearer token, so that an unusable client fails here.
// Calling [Session.Store] after it persists the minted token on disk.
func (s Session) Status(ctx context.Context) (status Status, err error) {
	status.Endpoint = s.internal.Endpoint
	status.MeshInfo, err = s.internal.MeshInfo()
	if err != nil {
		return
	}
	_, err = s.internal.GetBearerToken(ctx)
	return
}

// Store stores the resolved session into the current profile including credentials and cache.
func (s Session) Store(ctx context.Context) error {
	return s.internal.Store(ctx)
}
