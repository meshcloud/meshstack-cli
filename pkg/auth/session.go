package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

type (
	// ResolveSessionOptions carries the setting sources, the calling front end and the credential to insist on.
	ResolveSessionOptions = auth.ResolveSessionOptions
	// Session is a facade for auth.Session obtained by ResolveSession, exposing mainly an authorized Session.Client.
	// Consider using ResolveClient instead.
	Session struct {
		internal auth.Session
		// opts are kept so that Client resolves its settings from the same sources the session came from.
		opts ResolveSessionOptions
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
	result.opts = opts
	result.internal, err = auth.ResolveSession(ctx, opts)
	return
}

// Client builds the client and checks the backend version, if not skipped using [meshstack.SkipVersionCheckSetting].
func (s Session) Client(ctx context.Context) (client.Client, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Building client for endpoint %s with user agent %s authenticated by %T",
		s.internal.Endpoint, s.internal.HttpClient.UserAgent, s.internal.Credential))
	c := client.New(ctx, s.internal.Endpoint, s.internal.HttpClient.UserAgent, s.internal)
	if skipVersionCheck, err := s.opts.ResolveSetting(ctx, meshstack.SkipVersionCheckSetting); err != nil {
		return client.Client{}, err
	} else if skipVersionCheck {
		// Neither ResolveSession nor this method does any HTTP backend call if version check is skipped
		// which is important for Terraform provider behavior not blocking early on when backend is unreachable.
		return c, nil
	}
	// this checks the meshStack backend version
	_, err := s.internal.CheckedMeshInfo()
	if err != nil {
		return client.Client{}, err
	}
	if err := warnIfNewerReleasePresent(ctx, s.internal.ConfigDir, s.internal.HttpClient, s.opts); err != nil {
		slog.WarnContext(ctx, "Cannot check for a newer release on GitHub: "+err.Error())
	}
	return c, nil
}

// ResolveClient resolves a session and builds its client, authorized against the meshStack backend.
// Convenient wrapper for ResolveSession(...) -> Session.Client(...).
func ResolveClient(ctx context.Context, opts ResolveSessionOptions) (client.Client, error) {
	session, err := ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client(ctx)
}

// Status retrieves info from backend and also mints a bearer token, so that an unusable client fails here.
// Calling [Session.Store] after it persists the minted token on disk.
func (s Session) Status(ctx context.Context) (status Status, err error) {
	status.Endpoint = s.internal.Endpoint
	status.MeshInfo, err = s.internal.CheckedMeshInfo()
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
