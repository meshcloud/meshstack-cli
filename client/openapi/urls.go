package openapi

import "github.com/meshcloud/meshstack-cli/client/types/xurl"

var (
	// ReleasedUrl serves the API docs of the latest meshStack release.
	ReleasedUrl = xurl.MustParsef("https://docs.meshcloud.io/api/meshstack-openapi-docs.json")
	// DevUrl serves the API docs of what is merged to meshStack's develop branch.
	DevUrl = xurl.MustParsef("https://docs.dev.meshcloud.io/api/meshstack-openapi-docs.json")
)
