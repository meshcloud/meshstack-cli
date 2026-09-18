package setting

import (
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

type (
	// Setting is one declared input, identified by its environment variable key.
	Setting interface {
		EnvKey() string
		Help() string
		HelpMarkdown() string
	}
)

// The settings a front end may supply through a FrontendSource.
var (
	Endpoint         Setting = meshstack.EndpointSetting
	Workspace        Setting = meshstack.WorkspaceSetting
	SkipVersionCheck Setting = meshstack.SkipVersionCheckSetting

	ApiKeyClientId     Setting = auth.ApiKeyClientIdSetting
	ApiKeyClientSecret Setting = auth.ApiKeyClientSecretSetting
	ApiToken           Setting = auth.ApiTokenSetting
)
