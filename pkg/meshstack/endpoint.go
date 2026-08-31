package meshstack

import (
	"strings"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

func ParseEndpoint(raw string) (endpoint xurl.URL, err error) {
	if err = endpoint.UnmarshalText([]byte(strings.TrimSuffix(strings.TrimSpace(raw), "/"))); err != nil {
		return endpoint, diags.Errorf("the meshStack endpoint is not a valid URL",
			"%q: %v. It should read like https://api.example.meshcloud.io.", raw, err)
	}
	return endpoint, nil
}

// SameEndpoint reports whether two endpoints name the same meshStack. The path and the case
// of the host are ignored, and an endpoint with no host matches nothing, not even another
// one with no host.
func SameEndpoint(a, b xurl.URL) bool {
	return canonicalEndpoint(a) == canonicalEndpoint(b) && canonicalEndpoint(a) != ""
}

func canonicalEndpoint(u xurl.URL) string {
	if u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}
