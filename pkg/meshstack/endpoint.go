package meshstack

import (
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var Endpoint = setting.Setting[xurl.URL]{
	Env:   "MESHSTACK_ENDPOINT",
	Short: "The meshStack API to act against, such as https://api.example.meshcloud.io. Also read from MESHSTACK_ENDPOINT.",
	Long: "The meshStack API to act against, such as `https://api.example.meshcloud.io`, also read from " +
		"`MESHSTACK_ENDPOINT`.\n\n" +
		"A profile carries an endpoint of its own, which is used when nothing above it names one. There is no " +
		"default beyond that, so naming neither an endpoint nor a profile that has one is an error.",
	Parse: setting.ParseTextUnmarshaler[xurl.URL],
}
