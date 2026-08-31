package meshstack

import (
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

var Endpoint = setting.Value[xurl.URL]{
	EnvKey: "MESHSTACK_ENDPOINT",
	Short:  "The meshStack API to act against, such as https://api.example.meshcloud.io. Also read from MESHSTACK_ENDPOINT.",
	Parse:  ParseEndpoint,
}

var Workspace = setting.Value[string]{
	EnvKey: envKey,
	Short:  "The workspace to act in. Also read from MESHSTACK_WORKSPACE.",
	Long: "The workspace to act in, also read from `MESHSTACK_WORKSPACE`.\n\n" +
		"A browser login is bound to one workspace, because the workspace is a scope on the token, " +
		"so naming another one mints another token. An API key carries whatever workspace its issuer " +
		"gave it, and nothing re-scopes that.",
	Parse: setting.Text,
}
