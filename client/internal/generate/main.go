// Command generate writes the packages of the meshObject kinds below client/.
package main

import (
	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/openapi/generator"
)

func main() {
	generator.Main(generator.Config{
		Module: "github.com/meshcloud/meshstack-cli/client",
		Shared: "meshobject",
		Packages: []generator.Package{
			{Kind: "meshWorkspace", ApiVersion: "v2", Actions: []string{"list", "show", "create", "update", "delete"}},
			{Kind: "meshBuildingBlock", ApiVersion: "v2-preview", Actions: []string{"list", "show", "create", "update", "delete", "purge", "trigger-run"}},
			{Kind: "meshBuildingBlockRun", ApiVersion: "v1", Actions: []string{"list", "show", "logs"}},
		},
		MapType: mapType,
	})
}

const typesImport = "github.com/meshcloud/meshstack-cli/client/types"

func mapType(at generator.Location, _ *openapi.Schema) (generator.GoType, bool) {
	switch at {
	case generator.Location{Schema: "MeshBuildingBlockInput", Property: "value"}:
		return generator.GoType{Expr: "types.SecretOrAny", Import: typesImport, Container: true}, true
	case generator.Location{Schema: "MeshBuildingBlockOutput", Property: "value"},
		generator.Location{Schema: "MeshBuildingBlockRun.MeshBuildingBlockRunSpec.MeshBuildingBlockInputsForRun", Property: "value"}:
		return generator.GoType{Expr: "any", Container: true}, true
	}
	return generator.GoType{}, false
}
