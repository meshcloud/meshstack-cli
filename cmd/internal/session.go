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
		ExplicitSourcesOption: ExplicitSourcesOption(),
		Version:               Version,
		GitHubRepo:            "meshcloud/meshstack-cli",
	}
}

func ExplicitSourcesOption() setting.ExplicitSourcesOption {
	return setting.ExplicitSourcesOption{
		UseSettingsFrom: []setting.ExplicitSource{
			EndpointFlag.AsSource(),
			WorkspaceFlag.AsSource(),
			SkipVersionCheckFlag.AsSource(),
		},
	}
}
