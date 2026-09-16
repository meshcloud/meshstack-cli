package setting

import (
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type (
	// Setting is one declared input, identified by its environment variable name.
	Setting interface {
		EnvKey() string
		Help() string
		HelpMarkdown() string
	}
	// ExplicitSource is a source that outranks the environment, as a flag or a block attribute does.
	ExplicitSource = setting.ExplicitSource
	// ExplicitSourcesOption carries the sources a front end contributes to every resolution.
	ExplicitSourcesOption = setting.ExplicitSourcesOption
)

// SingleExplicitSource marks the given source as explicit during [setting.Setting.Resolve],
// which prefers values from this over env or default sources.
// Implement either [setting.Source] directly and use this wrapper fitting [setting.ExplicitSourcesOption.UseSettingsFrom],
// or use [ExplicitLookupSource] alternatively.
func SingleExplicitSource(source setting.Source) []setting.ExplicitSource {
	return []setting.ExplicitSource{{Source: source}}
}

// ExplicitLookupSource builds an ExplicitSource for a matching Setting.EnvKey() using a lookup func,
// which is lazily called when needed during resolution.
func ExplicitLookupSource(matchingEnvKey, description string, lookup func() (string, error)) setting.ExplicitSource {
	return setting.ExplicitSource{Source: setting.LookupSource{
		MatchingKey: matchingEnvKey,
		Description: description,
		Func:        lookup,
	}}
}

// The settings a front end (CLI, TF Provider) may read and supply.
var (
	Endpoint         Setting = meshstack.EndpointSetting
	SkipVersionCheck Setting = meshstack.SkipVersionCheckSetting

	ApiKeyClientId     Setting = auth.ApiKeyClientIdSetting
	ApiKeyClientSecret Setting = auth.ApiKeyClientSecretSetting
	ApiToken           Setting = auth.ApiTokenSetting
)
