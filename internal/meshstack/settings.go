package meshstack

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var EndpointSetting = setting.Setting[xurl.URL]{
	Env: "MESHSTACK_ENDPOINT",
	Short: func(envKey string) string {
		return fmt.Sprintf("The meshStack API to act against, such as https://api.example.meshcloud.io. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The meshStack API to act against, such as `https://api.example.meshcloud.io`, "+
			"also read from `%s` and inferred from current profile if possible.", envKey)
	},
	Parse: setting.ParseTextUnmarshaler[xurl.URL],
}

// SkipVersionCheckSetting skips both the minimum backend version check and the check for a newer
// release of the front end itself.
var SkipVersionCheckSetting = setting.Setting[bool]{
	Env: "MESHSTACK_SKIP_VERSION_CHECK",
	Short: func(envKey string) string {
		return fmt.Sprintf("Skip version check against meshStack backend. Also read from %s.", envKey)
	},
	Parse: setting.ParseBool,
	// An explicit "0", because ParseBool reads every string but a spelling of "no" as true.
	Default: setting.StaticDefault("0"),
}
