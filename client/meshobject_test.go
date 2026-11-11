package client

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/http"
)

func TestALinkToAnotherOriginIsNotFollowed(t *testing.T) {
	c := meshObjectClient[MeshWorkspace, NoFilter]{
		collection: &url.URL{Scheme: "https", Host: "meshstack.example", Path: "/api/meshobjects/meshworkspaces"},
	}
	for _, href := range []string{"https://other.host/api/x", "//other.host/x", "http://meshstack.example/x"} {
		_, err := c.followLink(t.Context(), http.MethodGet, href)
		require.ErrorContains(t, err, "which leaves the endpoint", href)
	}
}
