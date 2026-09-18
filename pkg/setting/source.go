package setting

import (
	"context"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type (
	// Sources are the setting sources a front end contributes to every resolution.
	Sources = setting.Sources
	// FrontendSource outranks the environment, as a flag or a block attribute does.
	FrontendSource = setting.FrontendSource
	// FallbackSource ranks below the environment, as a prompt does.
	FallbackSource = setting.FallbackSource

	// Workspaces are the workspaces reachable while resolving Workspace, see WorkspacesFromContext.
	Workspaces = meshstack.Workspaces
	// MeshWorkspace is one of them, and renders itself for a person to pick from.
	MeshWorkspace = meshstack.MeshWorkspace
)

// SingleSource marks the given source as highest precedence during [setting.Sources.ResolveSetting],
// which prefers values from this over env or default sources.
// Implement either [setting.Source] directly or use [LookupSource], [FallbackLookupSource] alternatively.
func SingleSource(source setting.Source) Sources {
	return Sources{setting.FrontendSource{Source: source}}
}

// LookupSource builds a source with the highest precedence during [setting.Sources.ResolveSetting]
// for a matching Setting.EnvKey() using a given lookup func,
// which is lazily called when needed during resolution.
func LookupSource(matchingEnvKey, description string, lookup func(ctx context.Context) (string, error)) setting.FrontendSource {
	return setting.FrontendSource{Source: setting.LookupSource{
		MatchingKey: matchingEnvKey,
		Description: description,
		Func:        lookup,
	}}
}

// FallbackLookupSource is a LookupSource with lower precedence than the environment variable when resolving the setting's value in [setting.Sources.ResolveSetting].
func FallbackLookupSource(matchingEnvKey, description string, lookup func(ctx context.Context) (string, error)) setting.FallbackSource {
	return setting.FallbackSource{Source: setting.LookupSource{
		MatchingKey: matchingEnvKey,
		Description: description,
		Func:        lookup,
	}}
}

// WorkspacesFromContext retrieves workspaces available during resolution in [setting.Source.Lookup],
// only call this during resolution for WorkspaceSetting.
func WorkspacesFromContext(ctx context.Context) (meshstack.Workspaces, error) {
	return meshstack.WorkspacesFromContext(ctx)
}
