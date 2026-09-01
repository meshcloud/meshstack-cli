package meshstack

import (
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

var Endpoint = setting.Value[xurl.URL]{
	EnvKey: "MESHSTACK_ENDPOINT",
	Short:  "The meshStack API to act against, such as https://api.example.meshcloud.io. Also read from MESHSTACK_ENDPOINT.",
	Long: "The meshStack API to act against, such as `https://api.example.meshcloud.io`, also read from " +
		"`MESHSTACK_ENDPOINT`.\n\n" +
		"A profile carries an endpoint of its own, which is used when nothing above it names one. There is no " +
		"default beyond that, so naming neither an endpoint nor a profile that has one is an error.",
	Parse: ParseEndpoint,
}

var Workspace = setting.Value[string]{
	EnvKey: envKey,
	Short:  "The workspace to act in. Also read from MESHSTACK_WORKSPACE.",
	Long: "The workspace to act in, also read from `MESHSTACK_WORKSPACE`.\n\n" +
		"A browser login has to name one, because the workspace is a scope on the token: a user access token is " +
		"minted for exactly one workspace, and naming another one mints another token. An API key or an API token " +
		"carries whatever workspace its issuer gave it, and nothing re-scopes that, so neither needs this set.\n\n" +
		"A profile's default workspace supplies it when nothing above it does.",
	Parse: setting.Text,
}
