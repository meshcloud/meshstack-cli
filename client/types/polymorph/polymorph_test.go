package polymorph_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/polymorph"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

type circle struct{}

type square struct {
	Side int `json:"side"`
}

type flat struct {
	polymorph.Polymorph[flat, string]

	Circle *circle `polymorph:"circle,inline"`
	Square *square `polymorph:"square,inline"`
}

type nested struct {
	polymorph.Polymorph[nested, string]

	Circle *circle `json:"circle" polymorph:"circle"`
	Square *square `json:"square" polymorph:"square"`
}

func TestPolymorph(t *testing.T) {
	t.Run("an inline subtype sits next to the type", func(t *testing.T) {
		encoded, err := json.Marshal(flat{Square: &square{Side: 2}})
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"square","side":2}`, string(encoded))

		var decoded flat
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		assert.Equal(t, flat{Type: "square", Square: &square{Side: 2}}, decoded)
	})

	t.Run("a nested subtype sits in its member, and one without fields needs none", func(t *testing.T) {
		encoded, err := json.Marshal(nested{Square: &square{Side: 2}})
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"square","square":{"side":2}}`, string(encoded))

		var decoded nested
		require.NoError(t, json.Unmarshal([]byte(`{"type":"circle"}`), &decoded))
		assert.Equal(t, nested{Type: "circle", Circle: &circle{}}, decoded)
		require.NoError(t, json.Unmarshal([]byte(`{"type":"square"}`), &decoded))
		assert.Equal(t, nested{Type: "square"}, decoded, "meshStack leaves out the subtype for a reader that may not see it")
	})

	t.Run("an unknown type decodes to the type alone", func(t *testing.T) {
		var decoded flat
		require.NoError(t, json.Unmarshal([]byte(`{"type":"triangle","corners":3}`), &decoded))
		assert.Equal(t, flat{Type: "triangle"}, decoded)
	})
}
