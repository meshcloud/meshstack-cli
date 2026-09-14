package setting

import (
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type (
	Setting interface {
		EnvKey() string
		Help() string
	}
	ExplicitSources = []setting.ExplicitSource
)

// ExplicitSource marks the given source as explicit during [setting.Setting.Resolve],
// which prefers values from this over env or default sources.
// See also [setting.ExplicitSourcesOption].
func ExplicitSource(source setting.Source) ExplicitSources {
	return ExplicitSources{setting.ExplicitSource{Source: source}}
}

var (
	Endpoint           Setting = meshstack.EndpointSetting
	ApiKeyClientId     Setting = auth.ApiKeyClientIdSetting
	ApiKeyClientSecret Setting = auth.ApiKeyClientSecretSetting
	ApiToken           Setting = auth.ApiTokenSetting
)
