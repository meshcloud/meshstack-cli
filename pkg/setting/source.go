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
	// MeshWorkspace is one workspace, and its String is what a person picks from.
	MeshWorkspace = meshstack.MeshWorkspace
)

// SingleSource wraps one source as the front end's only contribution, ranking it above the
// environment and the default.
func SingleSource(source setting.Source) Sources {
	return Sources{setting.FrontendSource{Source: source}}
}

// LookupSource builds a source for one setting that ranks above the environment. The lookup runs
// only when the resolution reaches it, so it may prompt or call the backend.
func LookupSource(matchingEnvKey, description string, lookup func(ctx context.Context) (string, error)) setting.FrontendSource {
	return setting.FrontendSource{Source: setting.LookupSource{
		MatchingKey: matchingEnvKey,
		Description: description,
		Func:        lookup,
	}}
}

// FallbackLookupSource is a LookupSource that ranks below the environment instead of above it.
func FallbackLookupSource(matchingEnvKey, description string, lookup func(ctx context.Context) (string, error)) setting.FallbackSource {
	return setting.FallbackSource{Source: setting.LookupSource{
		MatchingKey: matchingEnvKey,
		Description: description,
		Func:        lookup,
	}}
}

// WorkspacesFromContext returns the workspaces the credential can see. They are only in the
// context while Workspace is being resolved, so a lookup for any other setting gets an error.
func WorkspacesFromContext(ctx context.Context) (meshstack.Workspaces, error) {
	return meshstack.WorkspacesFromContext(ctx)
}
