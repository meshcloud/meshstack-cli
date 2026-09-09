package setting

import (
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

type (
	Setting interface {
		EnvKey() string
		Help() string
	}

	Source            = setting.ExplicitSource
	SourceDescription = setting.SourceDescription
)

var (
	Endpoint           Setting = meshstack.Endpoint
	Profile            Setting = profile.NameSetting
	Workspace          Setting = meshstack.Workspace
	ApiKeyClientId     Setting = credential.ApiKeyClientId
	ApiKeyClientSecret Setting = credential.ApiKeyClientSecret
	ApiBearerToken     Setting = credential.ApiBearerToken
	// TODO expose other settings used "externally" by CLI cmd package.
)
