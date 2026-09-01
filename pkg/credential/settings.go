package credential

import "github.com/meshcloud/meshstack-cli/pkg/setting"

var ApiKeyId = setting.Value[string]{
	EnvKey: "MESHSTACK_API_KEY",
	Short:  "The client id of a meshStack API key, which mints tokens together with its secret. Also read from MESHSTACK_API_KEY.",
	Long: "The client id of a meshStack API key, which mints tokens together with its secret, also read from " +
		"`MESHSTACK_API_KEY`.\n\n" +
		"An id decides the identity outright: whatever names it first is the credential, and a profile below it " +
		"contributes nothing but the secret it stores for that same id. Where no secret can be found for it, the " +
		"run fails rather than falling back to another credential.",
	Parse: setting.Text,
}

var ApiSecret = setting.Value[string]{
	EnvKey: "MESHSTACK_API_SECRET",
	Short:  "The client secret belonging to the API key. Also read from MESHSTACK_API_SECRET.",
	Long: "The client secret belonging to the API key, also read from `MESHSTACK_API_SECRET`.\n\n" +
		"A secret is paired with the id named beside it, or with an id named above it that brought no secret of " +
		"its own — which is what makes an id in one place and `MESHSTACK_API_SECRET` in another the ordinary " +
		"non-interactive setup. It is skipped where a *different* id is set alongside it, because a secret " +
		"sitting next to another id belongs to that id.\n\n" +
		"Where the profile supplies the identity as well, its own stored secret wins over a differing " +
		"`MESHSTACK_API_SECRET`, and a warning names both. Which of the two is newer is not knowable, so a " +
		"rotated secret is applied by logging in again rather than by exporting it.",
	Parse: setting.Text,
}

var ApiToken = setting.Value[string]{
	EnvKey: "MESHSTACK_API_TOKEN",
	Short:  "A meshStack access token to send as it is. Also read from MESHSTACK_API_TOKEN.",
	Long: "A meshStack access token to send as it is, also read from `MESHSTACK_API_TOKEN`. A building block run " +
		"has one injected under that name.\n\n" +
		"It replaces an API key and its secret, and nothing can mint a replacement from it: once the " +
		"token has expired, every request fails until another token is configured.\n\n" +
		"A token is an identity of its own rather than another spelling of an API key, so setting it beside an " +
		"API key id in the same place is an error naming both, not a precedence contest.",
	Parse: setting.Text,
}
