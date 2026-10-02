package testacc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccApiAsksForJsonOnlyWhereTheDocsListNoVersion holds the evidence for where meshstack api lets
// --request-json default the Accept header to application/json.
func TestAccApiAsksForJsonOnlyWhereTheDocsListNoVersion(t *testing.T) {
	c := loggedInWithApiKey(t)

	t.Run("a meshObject endpoint refuses application/json", func(t *testing.T) {
		output, err := c.run("", "api", "/api/meshobjects/meshworkspaces", "-H", "Accept: application/json")

		require.EqualErrorf(t, err, "meshStack answered HTTP 406. Run meshstack api-docs /api/meshobjects/meshworkspaces "+
			"-X GET -H 'Accept: application/json' to see what the API takes", "output:\n%s", output)
	})

	t.Run("--request-json reaches a meshObject endpoint in its media type", func(t *testing.T) {
		// A body of no metadata, which meshStack refuses before it creates anything.
		output, err := c.run(`{}`, "api", "-X", "POST", "/api/meshobjects/meshworkspaces", "--request-json", "-")

		require.ErrorContainsf(t, err, "meshStack answered HTTP 400", "output:\n%s", output)
		assert.Contains(t, output, "HttpMessageNotReadable", "meshStack took the media type and read the body")
	})
}
