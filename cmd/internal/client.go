package internal

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

// ResolveClient is the only place a client is built, so every command sends the same user agent
// and resolves the same global flags as settings.
func ResolveClient(ctx context.Context) (client.Client, error) {
	return auth.ResolveClient(ctx, resolveSessionOptions())
}
