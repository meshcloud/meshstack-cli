package setting

import (
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

type (
	// Setting is one declared input provided by a frontend (CLI, TF Provider, ...),
	// uniquely identified by its environment variable EnvKey.
	Setting interface {
		EnvKey() string
		Help() string
		HelpMarkdown() string
	}
)

// The settings a front end (CLI, TF Provider) may supply via FrontendSource.
var (
	Endpoint         Setting = meshstack.EndpointSetting
	Workspace        Setting = meshstack.WorkspaceSetting
	SkipVersionCheck Setting = meshstack.SkipVersionCheckSetting

	ApiKeyClientId     Setting = auth.ApiKeyClientIdSetting
	ApiKeyClientSecret Setting = auth.ApiKeyClientSecretSetting
	ApiToken           Setting = auth.ApiTokenSetting
)
