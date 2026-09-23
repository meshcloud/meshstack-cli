package client

import (
	"context"
	"time"

	"github.com/meshcloud/meshstack-cli/client/internal"
)

// WithPageTimeout bounds each page a listing on ctx fetches, its retries and token renewal
// included, rather than the listing as a whole. See [internal.WithPageTimeout].
func WithPageTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return internal.WithPageTimeout(ctx, timeout)
}
