package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

type (
	// ResolveClientOptions carries the setting sources and information about the calling frontend.
	ResolveClientOptions = auth.ResolveSessionOptions
	// Session is the profile, workspace and credential that ResolveClient builds its client from.
	Session = auth.Session
)

// ResolveSession is for a front end that needs the session itself, such as an authorization for
// requests that no meshStack client sends.
func ResolveSession(ctx context.Context, opts ResolveClientOptions) (Session, error) {
	return auth.ResolveSession(ctx, opts)
}

// ResolveClient resolves a session and builds its client in one call.
func ResolveClient(ctx context.Context, opts ResolveClientOptions) (client.Client, error) {
	session, err := ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client()
}

// ResolveWorkspace resolves the workspace that the client of ResolveClient works in: the one a
// setting names, else the profile's default workspace. It is empty where neither names one.
func ResolveWorkspace(ctx context.Context, opts ResolveClientOptions) (string, error) {
	workspace, err := auth.ResolveWorkspace(ctx, opts)
	return string(workspace), err
}
