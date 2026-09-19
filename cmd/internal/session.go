package internal

import (
	"context"

	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func ResolveSession(ctx context.Context, options ...ResolveSessionOption) (auth.Session, error) {
	opts := resolveSessionOptions()
	for _, option := range options {
		option(&opts)
	}
	return auth.ResolveSession(ctx, opts)
}

type ResolveSessionOption func(*auth.ResolveSessionOptions)

func resolveSessionOptions() auth.ResolveSessionOptions {
	return auth.ResolveSessionOptions{
		SettingSources: SettingSources(),
		Version:        Version,
		GitHubRepo:     "meshcloud/meshstack-cli",
	}
}

func SettingSources() setting.Sources {
	return setting.Sources{
		EndpointFlag.AsSource(),
		WorkspaceFlag.AsSource(),
		// An unset bool flag reads false, and a frontend source outranks the environment, so
		// contributing that false would mask MESHSTACK_SKIP_VERSION_CHECK.
		SkipVersionCheckFlag.AsSourceUnless(func(skip bool) bool { return !skip }),
		ProfileFlag.AsSource(),
	}
}
