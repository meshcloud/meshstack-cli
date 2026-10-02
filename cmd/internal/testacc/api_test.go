package testacc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
