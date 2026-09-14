package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth"
)

type ResolveSessionOptions = auth.ResolveSessionOptions

type Session interface {
	Client(ctx context.Context) (client.Client, error)
}

func ResolveSession(ctx context.Context, opts ResolveSessionOptions) (Session, error) {
	return auth.ResolveSession(ctx, opts)
}
