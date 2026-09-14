package meshstack

import (
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var EndpointSetting = setting.Setting[xurl.URL]{
	Env:   "MESHSTACK_ENDPOINT",
	Short: "The meshStack API to act against, such as https://api.example.meshcloud.io. Also read from MESHSTACK_ENDPOINT.",
	Long: "The meshStack API to act against, such as `https://api.example.meshcloud.io`, " +
		"also read from `MESHSTACK_ENDPOINT` and inferred from current profile if possible.",
	Parse: setting.ParseTextUnmarshaler[xurl.URL],
}
