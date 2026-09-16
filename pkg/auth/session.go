package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

type (
	// ResolveSessionOptions carries the setting sources, the user agent and the credential to insist on.
	ResolveSessionOptions = auth.ResolveSessionOptions
)

// Session is a resolved endpoint and credential, ready to build a client from or to store.
type Session struct {
	internal auth.Session
	// opts are kept so that Client resolves its settings from the same sources the session came from.
	opts ResolveSessionOptions
}

// ResolveSession resolves the profile (creating a default one if non exists) and a Session from it.
// It does not check the backend version, so prefer ResolveClient where the Session is not needed.
func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (result Session, err error) {
	result.opts = opts
	result.internal, err = auth.ResolveSession(ctx, opts)
	return
}

// ResolveClient resolves a session and builds its client.
func ResolveClient(ctx context.Context, opts ResolveSessionOptions) (client.Client, error) {
	session, err := ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client(ctx)
}

// Client builds the client and checks the backend version, if not skipped using [meshstack.SkipVersionCheckSetting].
func (s Session) Client(ctx context.Context) (client.Client, error) {
	c := s.internal.Client(ctx)
	skipVersionCheck, err := s.opts.ResolveSetting(meshstack.SkipVersionCheckSetting)
	if err != nil {
		return c, err
	}
	if skipVersionCheck {
		return c, nil
	}
	info, err := c.MeshInfo.Read(ctx)
	if err != nil {
		return c, err
	}
	return c, info.CheckVersion()
}

// Endpoint is the meshStack this session acts against.
func (s Session) Endpoint() xurl.URL {
	return s.internal.Endpoint
}

// Store stores the resolved session into the current profile including credentials and cache.
func (s Session) Store(ctx context.Context) error {
	return s.internal.Store(ctx)
}
