package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

type (
	// ResolveClientOptions carries the setting sources and information about the calling frontend.
	ResolveClientOptions = auth.ResolveSessionOptions
)

// ResolveClient resolves a session and builds its client in one call.
func ResolveClient(ctx context.Context, opts ResolveClientOptions) (client.Client, error) {
	session, err := auth.ResolveSession(ctx, opts)
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
