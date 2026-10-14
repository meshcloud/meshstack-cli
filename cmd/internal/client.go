package internal

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

type ResolveClientOptionsModifier func(*auth.ResolveClientOptions)

func ResolveClientOptions(modifiers ...ResolveClientOptionsModifier) (opts auth.ResolveClientOptions) {
	defer func() {
		for _, modifier := range modifiers {
			modifier(&opts)
		}
	}()
	return auth.ResolveClientOptions{
		SettingSources: SettingSources(),
		Version:        Version,
		GitHubRepo:     "meshcloud/meshstack-cli",
	}
}

// SkipVersionCheck is for a call whose failure only leaves something out: the version checks call
// meshStack and GitHub, and warn where they fail.
func SkipVersionCheck(opts *auth.ResolveClientOptions) {
	opts.SettingSources = append(opts.SettingSources, setting.LookupSource(setting.SkipVersionCheck.EnvKey(), "the calling command",
		func(context.Context) (string, error) { return "true", nil }))
}

// ResolveClient and ResolveSession are the only places a meshStack client is built, so every
// command resolves the same global flags as settings.
func ResolveClient(ctx context.Context, modifiers ...ResolveClientOptionsModifier) (client.Client, error) {
	return auth.ResolveClient(ctx, ResolveClientOptions(modifiers...))
}

func ResolveSession(ctx context.Context, modifiers ...ResolveClientOptionsModifier) (auth.Session, error) {
	return auth.ResolveSession(ctx, ResolveClientOptions(modifiers...))
}

func WithSettingSources(sources setting.Sources) ResolveClientOptionsModifier {
	return func(opts *auth.ResolveClientOptions) {
		opts.SettingSources = append(opts.SettingSources, sources...)
	}
}
