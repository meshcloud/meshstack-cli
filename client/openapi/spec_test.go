package openapi_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/openapi"
)

func TestSpec(t *testing.T) {
	document, spec := parseTestSpec(t)
	selectOperations := func(t *testing.T, selector openapi.Selector) []string {
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
			`POST /api/meshobjects/meshbuildingblocks ["application/json","application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "post", Path: "/api/meshobjects/meshbuildingblocks"}))
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblocks ["application/json","application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblocks", ApiVersion: openapi.MustParseApiVersion("v1")}))
	})

	t.Run("treats two templates of one path as versions of one operation", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{uuid} ["application/vnd.meshcloud.api.meshtenant.v4.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc"}))
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{tenantIdentifier} ["application/vnd.meshcloud.api.meshtenant.v3.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc", ApiVersion: openapi.MustParseApiVersion("v3")}))

		_, err := spec.Select(openapi.Selector{Method: "GET", Path: "/api/meshobjects/meshtenants/abc", ApiVersion: openapi.MustParseApiVersion("v5")})
		assert.EqualError(t, err, "GET /api/meshobjects/meshtenants/abc offers v3, v4, not v5")
	})

	t.Run("prefers a literal segment over a parameter of the same method", func(t *testing.T) {
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblockruns/{blockRunUuid}/status ["application/json"]`,
		}, selectOperations(t, openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblockruns/abc/status"}))
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblockruns/create ["application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json"]`,
			`GET /api/meshobjects/meshbuildingblockruns/{blockRunUuid} ["application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshbuildingblockruns/create"}),
			"a GET of .../create reaches the run with the uuid create")
	})

	t.Run("selects the latest version of each action of a kind for a method", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshbuildingblockruns/{blockRunUuid} ["application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json"]`,
			`GET /api/meshobjects/meshbuildingblockruns/{blockRunUuid}/plan-artifact ["application/octet-stream"]`,
			`GET /api/meshobjects/meshtenants/{uuid} ["application/vnd.meshcloud.api.meshtenant.v4.hal+json"]`,
		}, selectOperations(t, openapi.Selector{Method: "GET"}))
	})

	t.Run("accepts the media type of the latest version, or the first of no version", func(t *testing.T) {
		selected, err := spec.Select(openapi.Selector{Path: "/api/meshobjects/meshbuildingblocks"})
		require.NoError(t, err)
		latest, ok := selected.Operations[0].LatestMediaType()
		require.True(t, ok)
		assert.Equal(t, "application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json", latest.Name)
		assert.Equal(t, "v2-preview", selected.Operations[0].LatestApiVersion().String())

		selected, err = spec.Select(openapi.Selector{Path: "/api/meshobjects/meshbuildingblockruns/abc/status"})
		require.NoError(t, err)
		latest, ok = selected.Operations[0].LatestMediaType()
		require.True(t, ok)
		assert.Equal(t, "application/json", latest.Name)
		assert.Empty(t, selected.Operations[0].LatestApiVersion().String())
	})

	t.Run("selects every method of a path, and nothing of an unknown path", func(t *testing.T) {
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{uuid} ["application/vnd.meshcloud.api.meshtenant.v4.hal+json"]`,
			`DELETE /api/meshobjects/meshtenants/{uuid} []`,
		}, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshtenants/abc/"}))
		assert.Empty(t, selectOperations(t, openapi.Selector{Path: "/api/meshobjects/meshunknowns"}))
	})

	t.Run("names the operations of a kind below its path as the commands of the CLI do", func(t *testing.T) {
		assert.Equal(t, []string{"meshBuildingBlock", "meshBuildingBlockRun", "meshTenant"}, spec.Kinds())
		assert.Equal(t, []string{"create", "show", "status", "plan-artifact"}, spec.Actions("meshBuildingBlockRun"),
			"POST .../create is below the path of the kind, which GET .../{blockRunUuid} shows")
		assert.Equal(t, []string{
			`GET /api/meshobjects/meshtenants/{uuid} ["application/vnd.meshcloud.api.meshtenant.v4.hal+json"]`,
			`DELETE /api/meshobjects/meshtenants/{uuid} []`,
		}, selectOperations(t, openapi.Selector{Kind: "meshtenant"}), "the latest version of each action")
		assert.Equal(t, []string{
			`POST /api/meshobjects/meshbuildingblockruns/{blockRunUuid}/status ["application/json"]`,
		}, selectOperations(t, openapi.Selector{Kind: "meshBuildingBlockRun", Action: "status"}))
	})

	selected, selectErr := spec.Select(openapi.Selector{Method: "POST", Path: "/api/meshobjects/meshbuildingblocks", ApiVersion: openapi.MustParseApiVersion("v1")})
	require.NoError(t, selectErr)

	t.Run("marshals a selection to its operations and the components they reference", func(t *testing.T) {
		out, err := json.Marshal(selected)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"paths": {
				"/api/meshobjects/meshbuildingblocks": {
					"post": {
						"summary": "Create a building block",
						"description": "Creates a building block.",
						"operationId": "meshBuildingBlockPost",
						"parameters": [{"name": "Accept", "in": "header", "required": true, "schema": {"type": "string"}}],
						"requestBody": {"content": {"application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json": {"schema": {"$ref": "#/components/schemas/blockV1"}}}},
						"responses": {
							"201": {"description": "201", "content": {"application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json": {"schema": {"$ref": "#/components/schemas/blockV1"}}}},
							"400": {"description": "400", "content": {"application/json": {"schema": {"type": "object"}}}}
						}
					}
				}
			},
			"components": {
				"schemas": {
					"blockV1": {"type": "object", "properties": {"spec": {"$ref": "#/components/schemas/specV1"}}},
					"specV1": {
						"type": "object",
						"required": ["name"],
						"properties": {
							"name": {"type": "string", "description": "The name."},
							"tags": {"type": "array", "items": {"$ref": "#/components/schemas/tagV1"}},
							"parent": {"$ref": "#/components/schemas/specV1"}
						}
					},
					"tagV1": {"type": "object", "properties": {"key": {"type": "string", "format": "uuid"}}}
				}
			}
		}`, string(out))
	})

	t.Run("reads an operation's parameters and the fields of its bodies, naming a referenced schema, up to a schema that contains itself", func(t *testing.T) {
		doc, err := selected.Doc(selected.Operations[0])
		require.NoError(t, err)
		assert.Equal(t, "Create a building block", doc.Summary)
		assert.Equal(t, []openapi.Parameter{{Name: "Accept", In: "header", Required: true, Type: "string"}}, doc.Parameters)
		fields := []openapi.Field{
			{Path: "spec", Type: "specV1"},
			{Path: "spec.name", Type: "string", Required: true, Description: "The name."},
			{Path: "spec.tags", Type: "array of tagV1"},
			{Path: "spec.tags[].key", Type: "string (uuid)"},
			{Path: "spec.parent", Type: "specV1"},
		}
		assert.Equal(t, []openapi.Body{{MediaType: "application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json", Fields: fields}}, doc.RequestBodies)
		assert.Equal(t, []openapi.Response{
			{Status: "201", Description: "201", Bodies: []openapi.Body{{MediaType: "application/vnd.meshcloud.api.meshbuildingblock.v1.hal+json", Fields: fields}}},
			{Status: "400", Description: "400", Bodies: []openapi.Body{{MediaType: "application/json"}}},
		}, doc.Responses)
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

func parseTestSpec(t *testing.T) ([]byte, openapi.Spec) {
	t.Helper()
	document, err := os.ReadFile("testdata/spec.json")
	require.NoError(t, err)
	spec, err := openapi.Parse(bytes.NewReader(document))
	require.NoError(t, err)
	return document, spec
}

func jsonString(t *testing.T, value any) string {
	t.Helper()
	out, err := json.Marshal(value)
	require.NoError(t, err)
	return string(out)
}
