package generator_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/openapi/generator"
)

var update = flag.Bool("update", false, "write the golden files anew")

func TestGenerate(t *testing.T) {
	document, err := os.Open("testdata/spec.json")
	require.NoError(t, err)
	spec, err := openapi.Parse(document)
	require.NoError(t, err)
	config := generator.Config{
		Module: "example.com/client",
		Shared: "meshobject",
		Packages: []generator.Package{
			{Kind: "meshThing", ApiVersion: "v2", Actions: []string{"list", "show", "create", "delete", "paint"}},
			{Kind: "meshOther", ApiVersion: "v1", Actions: []string{"show"}},
		},
	}

	t.Run("fails on a oneOf without discriminator that the TypeMapper leaves", func(t *testing.T) {
		_, err := generator.Generate(spec, config)
		assert.EqualError(t, err, "GET /api/meshobjects/meshthings: response 200: MeshThingSpecV2.value: oneOf without a discriminator has no Go type; the TypeMapper must map it")
	})

	t.Run("writes each package as the golden file has it", func(t *testing.T) {
		config.MapType = func(at generator.Location, _ *openapi.Schema) (generator.GoType, bool) {
			if at == (generator.Location{Schema: "MeshThingSpecV2", Property: "value"}) {
				return generator.GoType{Expr: "types.SecretOrAny", Import: "example.com/client/types", Container: true}, true
			}
			return generator.GoType{}, false
		}
		files, err := generator.Generate(spec, config)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"meshthing/v2/generated.go", "meshother/v1/generated.go", "meshobject/generated.go"}, keys(files))
		for name, content := range files {
			golden := filepath.Join("testdata", "golden", filepath.FromSlash(name)+".golden")
			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o750))
				require.NoError(t, os.WriteFile(golden, content, 0o600))
			}
			expected, err := os.ReadFile(golden) //nolint:gosec // G304: a golden file below testdata
			require.NoError(t, err)
			assert.Equal(t, string(expected), string(content), name)
		}
	})
}

func keys(files map[string][]byte) []string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	return names
}
