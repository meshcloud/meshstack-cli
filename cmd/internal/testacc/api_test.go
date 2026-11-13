package testacc

import (
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
		require.ErrorContainsf(t, err, "HttpMediaTypeNotAcceptable",
			"a meshObject endpoint refuses application/json, output:\n%s", output)
		require.ErrorContains(t, err, "meshstack api-docs /api/meshobjects/meshworkspaces -X GET -H 'Accept: application/json'")

		// A body of no metadata, which meshStack refuses before it creates anything.
		output, err = c.run(`{}`, "api", "-X", "POST", "/api/meshobjects/meshworkspaces", "--request-json", "-")
		require.ErrorContainsf(t, err, "400", "output:\n%s", output)
		assert.Contains(t, output, "HttpMessageNotReadable", "--request-json reaches a meshObject endpoint in its media type, which meshStack took to read the body")
	}
}

// The API docs list no version for a delete, so request.negotiate sends no Accept and Content-Type.
func apiDeletesWithNoMediaType(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		output, err := c.run("", "api", "-X", "DELETE", "/api/meshobjects/meshtenants/00000000-0000-4000-8000-000000000000")

		require.ErrorContainsf(t, err, "meshStack answered HTTP 404", "a tenant that does not exist is not found, rather than refused for its headers, output:\n%s", output)
		assert.NotContains(t, err.Error(), "api-docs", "a 404 on a path the API docs list has no hint to them")
	}
}

func apiFollowsASelfLink(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		var href string
		for workspace, err := range c.client(t).Workspace.List(t.Context(), client.NoFilter{}, client.WithPageSize(1)) {
			require.NoError(t, err)
			href = workspace.Links["self"].Href
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
