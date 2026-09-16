package internal

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

// ResolveClient is the only place where a client is constructed, that ensures we always use the same user agent and
// always pass in relevant global flags as settings, see resolveSessionOptions.
func ResolveClient(ctx context.Context) (client.Client, error) {
	return auth.ResolveClient(ctx, resolveSessionOptions())
}
