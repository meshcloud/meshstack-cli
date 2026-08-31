package credential

import "github.com/meshcloud/meshstack-cli/pkg/setting"

var ApiKeyId = setting.Value[string]{
	EnvKey: "MESHSTACK_API_KEY",
	Short:  "The client id of a meshStack API key, which mints tokens together with its secret. Also read from MESHSTACK_API_KEY.",
	Parse:  setting.Text,
}

var ApiSecret = setting.Value[string]{
	EnvKey: "MESHSTACK_API_SECRET",
	Short:  "The client secret belonging to the API key. Also read from MESHSTACK_API_SECRET.",
	Parse:  setting.Text,
}

var ApiToken = setting.Value[string]{
	EnvKey: "MESHSTACK_API_TOKEN",
	Short:  "A meshStack access token to send as it is. Also read from MESHSTACK_API_TOKEN.",
	Long: "A meshStack access token to send as it is, also read from `MESHSTACK_API_TOKEN`.\n\n" +
		"It replaces an API key and its secret, and nothing can mint a replacement from it: once the " +
		"token has expired, every request fails until another token is configured.",
	Parse: setting.Text,
}
