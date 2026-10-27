package meshobject_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/meshobject"
)

func TestLinks(t *testing.T) {
	var links meshobject.Links
	require.NoError(t, json.Unmarshal([]byte(`{
		"self": {"href": "https://meshstack/api/meshobjects/meshbuildingblockruns/r1"},
		"updateSource": {"href": "https://meshstack/api/meshobjects/meshbuildingblockruns/r1/status/source/{sourceId}", "templated": true},
		"runs": {"href": "https://meshstack/api/meshobjects/meshbuildingblockruns{?buildingBlockUuid,sort}", "templated": true},
		"items": [{"href": "https://meshstack/a"}, {"href": "https://meshstack/b"}]
	}`), &links))

	t.Run("reads one link and an array of links", func(t *testing.T) {
		href, ok := links.Href("self")
		assert.True(t, ok)
		assert.Equal(t, "https://meshstack/api/meshobjects/meshbuildingblockruns/r1", href)
		assert.Len(t, links["items"], 2)
		_, ok = links.Href("missing")
		assert.False(t, ok)
	})

	t.Run("expands a template, leaving out what vars lacks", func(t *testing.T) {
		expanded, err := links.Expand("updateSource", map[string]string{"sourceId": "a b/c"})
		require.NoError(t, err)
		assert.Equal(t, "https://meshstack/api/meshobjects/meshbuildingblockruns/r1/status/source/a%20b%2Fc", expanded)

		expanded, err = links.Expand("runs", map[string]string{"buildingBlockUuid": "b1"})
		require.NoError(t, err)
		assert.Equal(t, "https://meshstack/api/meshobjects/meshbuildingblockruns?buildingBlockUuid=b1", expanded)

		expanded, err = links.Expand("self", nil)
		require.NoError(t, err)
		assert.Equal(t, "https://meshstack/api/meshobjects/meshbuildingblockruns/r1", expanded)
	})

	t.Run("writes one link as an object again", func(t *testing.T) {
		out, err := json.Marshal(meshobject.Links{"self": links["self"], "items": links["items"]})
		require.NoError(t, err)
		assert.JSONEq(t, `{"self": {"href": "https://meshstack/api/meshobjects/meshbuildingblockruns/r1"}, "items": [{"href": "https://meshstack/a"}, {"href": "https://meshstack/b"}]}`, string(out))
	})
}
