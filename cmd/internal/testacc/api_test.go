package testacc

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
)

// apiAsksForJson checks the answers of meshStack that request.negotiate in cmd/api relies on where
// it defaults the Accept header to application/json.
func apiAsksForJson(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		output, err := c.run("", "api", "/api/meshobjects/meshworkspaces", "-H", "Accept: application/json")
		require.EqualErrorf(t, err, "meshStack answered HTTP 406. Run meshstack api-docs /api/meshobjects/meshworkspaces "+
			"-X GET -H 'Accept: application/json' to see what the API takes", "a meshObject endpoint refuses application/json, output:\n%s", output)

		// A body of no metadata, which meshStack refuses before it creates anything.
		output, err = c.run(`{}`, "api", "-X", "POST", "/api/meshobjects/meshworkspaces", "--request-json", "-")
		require.ErrorContainsf(t, err, "meshStack answered HTTP 400", "output:\n%s", output)
		assert.Contains(t, output, "HttpMessageNotReadable", "--request-json reaches a meshObject endpoint in its media type, which meshStack took to read the body")
	}
}

func apiFollowsASelfLink(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		var href string
		for item, err := range c.client(t).Raw.List[client.MeshWorkspace](t.Context(), nil, client.ListOptions{PageSize: 1}) {
			require.NoError(t, err)
			var links struct {
				Links struct {
					Self struct {
						Href string `json:"href"`
					} `json:"self"`
				} `json:"_links"`
			}
			require.NoError(t, json.Unmarshal(item, &links))
			href = links.Links.Self.Href
			break
		}
		require.NotEmpty(t, href, "the workspace list answers no workspace with a self link")

		withoutEndpoint := *c
		withoutEndpoint.endpoint = ""
		output, err := withoutEndpoint.run("", "api", href)

		require.NoErrorf(t, err, "output:\n%s", output)
		assert.Contains(t, output, `"href": "`+href+`"`, "the answer is the workspace the link is for")
	}
}
