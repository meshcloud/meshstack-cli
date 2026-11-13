package openapi_test

import (
	"bytes"
	"encoding/json/jsontext"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

const (
	runV1    = "application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json"
	tenantV3 = "application/vnd.meshcloud.api.meshtenant.v3.hal+json"
	tenantV4 = "application/vnd.meshcloud.api.meshtenant.v4.hal+json"
)

func TestSpec(t *testing.T) {
	document, err := os.ReadFile("testdata/spec.json")
	require.NoError(t, err)
	spec := parseSpec(t, document)
	selectOperations := selectOperationsOf(spec)

	t.Run("marshals to the document as it came without a selector", func(t *testing.T) {
		selected, err := spec.Select(openapi.Selector{})
		require.NoError(t, err)
		out, err := json.Marshal(selected)
		require.NoError(t, err)
		compacted := jsontext.Value(document)
		require.NoError(t, compacted.Compact())
		assert.Equal(t, string(compacted), string(out), "in the document's order")
	})

	t.Run("selects the latest version of a path, preview included, unless a version is named", func(t *testing.T) {
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblocks ["application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "post", Path: "/api/meshobjects/meshbuildingblocks"}))
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblocks ["application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblocks", ApiVersion: openapi.MustParseApiVersion("v1")}))
	})

	t.Run("keeps the media types of one version of an operation that offers several, and fails for another version", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{tenant} ["` + tenantV4 + `"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc"}))
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{tenant} ["` + tenantV3 + `"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc", ApiVersion: openapi.MustParseApiVersion("v3")}))

		_, err := spec.Select(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc", ApiVersion: openapi.MustParseApiVersion("v5")})
		assert.EqualError(t, err, "GET /api/meshobjects/meshtenants/abc offers v3, v4, not v5")
	})

	t.Run("selects the latest version of each action of a kind for a method", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshbuildingblockruns/{buildingBlockRun} ["` + runV1 + `"]`,
			`GET /api/meshobjects/meshbuildingblockruns/{buildingBlockRun}/plan-artifact ["application/octet-stream"]`,
			`GET /api/meshobjects/meshtenants/{tenant} ["` + tenantV4 + `"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET"}))
	})

	t.Run("accepts the media type of the latest version, or the first of no version, and never that of an error response", func(t *testing.T) {
		selected, err := spec.Select(openapi.Selector{Path: "/api/meshobjects/meshbuildingblocks"})
		require.NoError(t, err)
		latest, ok := selected.Operations[0].LatestMediaType()
		require.True(t, ok)
		assert.Equal(t, "application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json", latest.Name)
		assert.Equal(t, "v2-preview", selected.Operations[0].LatestApiVersion().String())

		selected, err = spec.Select(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshbuildingblockruns/abc/plan-artifact"})
		require.NoError(t, err)
		latest, ok = selected.Operations[0].LatestMediaType()
		require.True(t, ok)
		assert.Equal(t, "application/octet-stream", latest.Name)
		assert.Empty(t, selected.Operations[0].LatestApiVersion().String())

		selected, err = spec.Select(openapi.Selector{Method: "DELETE", Path: "/api/meshobjects/meshtenants/abc"})
		require.NoError(t, err)
		_, ok = selected.Operations[0].LatestMediaType()
		assert.False(t, ok)
	})

	t.Run("selects every operation whose template matches a path, of any method, and nothing of an unknown path", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{tenant} ["` + tenantV4 + `"]`,
			`DELETE /api/meshobjects/meshtenants/{tenant} []`,
		}, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshtenants/abc/"}))
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblockruns/create ["` + runV1 + `"]`,
			`GET /api/meshobjects/meshbuildingblockruns/{buildingBlockRun} ["` + runV1 + `"]`,
		}, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshbuildingblockruns/create"}),
			"a GET of .../create reaches the run with the uuid create")
		assert.Empty(t, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshunknowns"}))
	})

	t.Run("names the operations of a kind below its path as the commands of the CLI do", func(t *testing.T) {
		assert.Equal(t, []string{"meshBuildingBlock", "meshBuildingBlockRun", "meshTenant"}, spec.Kinds())
		assert.Equal(t, []string{"create", "show", "abort", "plan-artifact", "plan-artifact-update"}, spec.Actions("meshBuildingBlockRun"),
			"POST .../create is below the path of the kind, which GET .../{buildingBlockRun} shows")
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{tenant} ["` + tenantV4 + `"]`,
			`DELETE /api/meshobjects/meshtenants/{tenant} []`,
		}, selectOperations(t, openapi.Selector{Kind: "meshtenant"}), "the latest version of each action")
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblockruns/{buildingBlockRun}/abort ["` + runV1 + `"]`,
		}, selectOperations(t, openapi.Selector{Kind: "meshBuildingBlockRun", Action: "abort"}))
		assert.Equal(t, []string{
			`PUT /api/meshobjects/meshbuildingblockruns/{buildingBlockRun}/plan-artifact ["application/octet-stream"]`,
		}, selectOperations(t, openapi.Selector{Kind: "meshBuildingBlockRun", Action: "plan-artifact-update"}))
	})

	t.Run("reads the parameters of an operation and of its path, following a $ref, with the path key of the selected version", func(t *testing.T) {
		parameters := func(selector openapi.Selector) []openapi.Parameter {
			selected, err := spec.Select(selector)
			require.NoError(t, err)
			require.Len(t, selected.Operations, 1)
			doc, err := selected.Doc(selected.Operations[0])
			require.NoError(t, err)
			return doc.Parameters
		}
		assert.Equal(t, []openapi.Parameter{
			{Name: "buildingBlockRun", In: "path", Required: true, Type: "string (uuid)", Description: "The UUID of the meshBuildingBlockRun.", PathKey: "metadata.uuid"},
		}, parameters(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshbuildingblockruns/abc/plan-artifact"}))
		assert.Equal(t, []openapi.Parameter{
			{Name: "tenant", In: "path", Required: true, Type: "string", Description: "DELETE takes the UUID or the full tenant identifier.", PathKey: "metadata.uuid"},
			{Name: "purge", In: "query", Type: "boolean", Description: "Removes the meshTenant from meshStack at once."},
		}, parameters(openapi.Selector{Method: "DELETE", Path: "/api/meshobjects/meshtenants/abc"}))
		assert.Equal(t, "metadata.uuid", parameters(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc"})[0].PathKey)
		assert.Equal(t, "metadata.tenantIdentifier",
			parameters(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc", ApiVersion: openapi.MustParseApiVersion("v3")})[0].PathKey)
	})

	selected, selectErr := spec.Select(openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblocks", ApiVersion: openapi.MustParseApiVersion("v1")})
	require.NoError(t, selectErr)

	t.Run("marshals a selection to its operations, with each $ref response in place, and the components they reference", func(t *testing.T) {
		out, err := json.Marshal(selected)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"paths": {
				"/api/meshobjects/meshbuildingblocks": {
					"post": {
						"tags": ["Building Blocks"],
						"summary": "Create building block",
						"description": "Create a new meshBuildingBlock with inputs and parent building block relationships",
						"operationId": "meshBuildingBlockPost",
						"requestBody": {
							"content": {
								"application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json": {
									"schema": {"$ref": "#/components/schemas/MeshBuildingBlockV1Request"},
									"x-meshstack-lifecycle": {"meshObject": "meshBuildingBlock.v1", "state": "GA", "notice": "v2 also supports building blocks on workspace level."}
								}
							},
							"required": true
						},
						"responses": {
							"201": {
								"description": "Building block created",
								"content": {"application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json": {"x-meshstack-lifecycle": {"meshObject": "meshBuildingBlock.v1", "state": "GA"}}}
							},
							"400": {
								"description": "The request is invalid. The message names the invalid part.",
								"content": {"application/json": {"schema": {"$ref": "#/components/schemas/CustomErrorMessage"}}}
							}
						},
						"x-position": 8
					}
				}
			},
			"components": {
				"schemas": {
					"CustomErrorMessage": {
						"required": ["errorCode", "message"],
						"type": "object",
						"properties": {"errorCode": {"type": "string"}, "message": {"type": "string", "description": "A message that describes the error."}},
						"description": "The body of every error response."
					},
					"MeshBuildingBlockIOV1": {
						"required": ["key"],
						"type": "object",
						"properties": {
							"key": {"type": "string", "description": "The key of the input as set on the related Building Block Definition."},
							"value": {"description": "The value, which is set on this input.", "nullable": true, "oneOf": [{"type": "string"}, {"$ref": "#/components/schemas/integer_int64"}, {"type": "boolean"}, {"$ref": "#/components/schemas/array_string_0"}]}
						}
					},
					"MeshBuildingBlockSpecV1": {
						"required": ["displayName", "parentBuildingBlocks"],
						"type": "object",
						"properties": {
							"displayName": {"type": "string", "description": "The display name of the Building Block."},
							"parentBuildingBlocks": {"description": "The parent building blocks.", "allOf": [{"$ref": "#/components/schemas/array_MeshParentBuildingBlock"}]},
							"inputs": {"description": "List of inputs, which are set on the Building Block.", "allOf": [{"$ref": "#/components/schemas/array_MeshBuildingBlockIOV1"}]}
						}
					},
					"MeshBuildingBlockV1Request": {
						"required": ["kind", "spec"],
						"type": "object",
						"properties": {
							"kind": {"type": "string", "enum": ["meshBuildingBlock"]},
							"apiVersion": {"type": "string", "enum": ["v1"]},
							"spec": {"description": "All fields in this section describe the Building Block.", "allOf": [{"$ref": "#/components/schemas/MeshBuildingBlockSpecV1"}]}
						}
					},
					"MeshParentBuildingBlock": {
						"required": ["buildingBlockUuid"],
						"type": "object",
						"properties": {"buildingBlockUuid": {"description": "The UUID of the parent Building Block.", "allOf": [{"$ref": "#/components/schemas/string_uuid"}]}}
					},
					"array_MeshBuildingBlockIOV1": {"type": "array", "items": {"$ref": "#/components/schemas/MeshBuildingBlockIOV1"}},
					"array_MeshParentBuildingBlock": {"type": "array", "items": {"$ref": "#/components/schemas/MeshParentBuildingBlock"}},
					"array_string_0": {"type": "array", "items": {"type": "string"}},
					"integer_int64": {"type": "integer", "format": "int64"},
					"string_uuid": {"type": "string", "format": "uuid"}
				}
			}
		}`, string(out))
	})

	t.Run("reads the fields of an operation's bodies, naming a referenced schema and each variant of a oneOf", func(t *testing.T) {
		doc, err := selected.Doc(selected.Operations[0])
		require.NoError(t, err)
		assert.Equal(t, "Create building block", doc.Summary)
		assert.Empty(t, doc.Parameters)
		assert.Equal(t, []openapi.Body{{MediaType: "application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json", Fields: []openapi.Field{
			{Path: "kind", Type: "string", Required: true},
			{Path: "apiVersion", Type: "string"},
			{Path: "spec", Type: "MeshBuildingBlockSpecV1", Required: true, Description: "All fields in this section describe the Building Block."},
			{Path: "spec.displayName", Type: "string", Required: true, Description: "The display name of the Building Block."},
			{Path: "spec.parentBuildingBlocks", Type: "array of MeshParentBuildingBlock", Required: true, Description: "The parent building blocks."},
			{Path: "spec.parentBuildingBlocks[].buildingBlockUuid", Type: "string (uuid)", Required: true, Description: "The UUID of the parent Building Block."},
			{Path: "spec.inputs", Type: "array of MeshBuildingBlockIOV1", Description: "List of inputs, which are set on the Building Block."},
			{Path: "spec.inputs[].key", Type: "string", Required: true, Description: "The key of the input as set on the related Building Block Definition."},
			{Path: "spec.inputs[].value", Type: "one of string, integer (int64), boolean, array of string", Description: "The value, which is set on this input."},
		}}}, doc.RequestBodies)
		assert.Equal(t, []openapi.Response{
			{Status: "201", Description: "Building block created", Bodies: []openapi.Body{{MediaType: "application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json"}}},
			{Status: "400", Description: "The request is invalid. The message names the invalid part.", Bodies: []openapi.Body{{MediaType: "application/json", Fields: []openapi.Field{
				{Path: "errorCode", Type: "string", Required: true},
				{Path: "message", Type: "string", Required: true, Description: "A message that describes the error."},
			}}}},
		}, doc.Responses)
	})

	t.Run("reads the fields of a map's values below the map", func(t *testing.T) {
		v2Preview, err := spec.Select(openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblocks"})
		require.NoError(t, err)
		doc, err := v2Preview.Doc(v2Preview.Operations[0])
		require.NoError(t, err)
		assert.Equal(t, []openapi.Field{
			{Path: "spec.inputs", Type: "map of MeshBuildingBlockInput", Required: true, Description: "Map of input keys to their input data."},
			{Path: "spec.inputs{}.value", Type: "one of string, integer (int64), boolean, array of string", Description: "The value of this input."},
			{Path: "spec.inputs{}.isOptional", Type: "boolean", Required: true, Description: "Whether the Building Block Definition allows this input to be left unset."},
		}, doc.RequestBodies[0].Fields[3:])
	})

	t.Run("adds the kind and apiVersion a body lacks, with the kind in camel case", func(t *testing.T) {
		mediaType, ok := selected.Operations[0].MediaType(openapi.MustParseApiVersion("v1"))
		require.True(t, ok)

		body, err := mediaType.WithKindAndApiVersion(jsontext.Value(`{"kind": "meshBuildingBlock", "spec": {}}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"apiVersion": "v1", "kind": "meshBuildingBlock", "spec": {}}`, string(body))

		_, err = mediaType.WithKindAndApiVersion(jsontext.Value(`{"apiVersion": "v2-preview"}`))
		assert.EqualError(t, err, `the body's apiVersion is "v2-preview", but application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json takes "v1"`)
	})
}

func TestDocOfASchemaThatContainsItself(t *testing.T) {
	spec := parseSpec(t, []byte(`{
		"openapi": "3.0.1",
		"paths": {
			"/api/nodes": {
				"get": {"responses": {"200": {"description": "200", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/node"}}}}}}
			}
		},
		"components": {
			"schemas": {
				"node": {"type": "object", "properties": {"name": {"type": "string"}, "parent": {"allOf": [{"$ref": "#/components/schemas/node"}]}, "children": {"type": "object", "additionalProperties": {"$ref": "#/components/schemas/node"}}}}
			}
		}
	}`))
	doc, err := spec.Doc(spec.Operations[0])
	require.NoError(t, err)
	assert.Equal(t, []openapi.Response{{Status: "200", Description: "200", Bodies: []openapi.Body{{MediaType: "application/json", Fields: []openapi.Field{
		{Path: "name", Type: "string"},
		{Path: "parent", Type: "node"},
		{Path: "children", Type: "map of node"},
	}}}}}, doc.Responses)
}

func TestApiVersion(t *testing.T) {
	versions := []string{"v1", "v2-preview", "v2", "v10"}
	for i := range len(versions) - 1 {
		assert.Negative(t, openapi.MustParseApiVersion(versions[i]).Compare(openapi.MustParseApiVersion(versions[i+1])), versions[i])
	}
	for _, invalid := range []string{"", "1", "v0", "vx", "v1-beta"} {
		_, err := openapi.ParseApiVersion(invalid)
		assert.Error(t, err, invalid)
	}
}

func parseSpec(t *testing.T, document []byte) openapi.Spec {
	t.Helper()
	spec, err := openapi.Parse(bytes.NewReader(document))
	require.NoError(t, err)
	return spec
}

func selectOperationsOf(spec openapi.Spec) func(t *testing.T, selector openapi.Selector) []string {
	return func(t *testing.T, selector openapi.Selector) []string {
		t.Helper()
		selected, err := spec.Select(selector)
		require.NoError(t, err)
		var operations []string
		for _, operation := range selected.Operations {
			var mediaTypes []string
			for _, mediaType := range operation.MediaTypes {
				mediaTypes = append(mediaTypes, mediaType.Name)
			}
			operations = append(operations, operation.Method+" "+string(operation.PathTemplate)+" "+jsonString(t, mediaTypes))
		}
		return operations
	}
}

func jsonString(t *testing.T, value any) string {
	t.Helper()
	out, err := json.Marshal(value)
	require.NoError(t, err)
	return string(out)
}
