package internal_test

import (
	"encoding/json/jsontext"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func items(values ...string) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		for _, value := range values {
			if !yield(jsontext.Value(value), nil) {
				return
			}
		}
	}
}

// We fetch JSON from the server which we then convert to YAML. The YAML conversion must not re-order, remove or add any properties.
func TestYamlWritesEachItemAsTheServerSentIt(t *testing.T) {
	platformTeam := `
	{
		"kind": "meshWorkspace",
		"apiVersion": "v2",
		"metadata": {"name": "platform-team"},
		"spec": {"displayName": "Platform Team"},
		"_links": {
			"self": {"href": "http://localhost:8080/api/meshobjects/meshworkspaces/platform-team"}
		}
	}`
	appTeam := `
	{
		"kind": "meshWorkspace",
		"apiVersion": "v2",
		"metadata": {"name": "app-team"}
	}`
	var out strings.Builder

	err := internal.WriteList(&out, internal.OutputYaml, items(platformTeam, appTeam))

	require.NoError(t, err)
	want := strings.TrimPrefix(`
kind: meshWorkspace
apiVersion: v2
metadata:
  name: platform-team
spec:
  displayName: Platform Team
_links:
  self:
    href: http://localhost:8080/api/meshobjects/meshworkspaces/platform-team
---
kind: meshWorkspace
apiVersion: v2
metadata:
  name: app-team
`, "\n")
	assert.Equal(t, want, out.String())
}

func TestNdjsonWritesEachItemOnALineOfItsOwn(t *testing.T) {
	platformTeam := `
	{
		"kind": "meshWorkspace",
		"metadata": {"name": "platform-team", "deletedOn": null}
	}`
	appTeam := `
	{
		"kind": "meshWorkspace",
		"metadata": {"name": "app-team"}
	}`
	var out strings.Builder

	err := internal.WriteList(&out, internal.OutputNdjson, items(platformTeam, appTeam))

	require.NoError(t, err)
	assert.Equal(t, []string{
		`{"kind":"meshWorkspace","metadata":{"name":"platform-team","deletedOn":null}}`,
		`{"kind":"meshWorkspace","metadata":{"name":"app-team"}}`,
		"",
	}, strings.Split(out.String(), "\n"))
}

func TestAFailingPageStopsTheListing(t *testing.T) {
	pageErr := errors.New("page 1 failed")
	failing := func(yield func(jsontext.Value, error) bool) {
		if yield(jsontext.Value(`{"kind":"meshWorkspace"}`), nil) {
			yield(nil, pageErr)
		}
	}
	var out strings.Builder

	err := internal.WriteList(&out, internal.OutputNdjson, failing)

	require.ErrorIs(t, err, pageErr)
	assert.Equal(t, []string{`{"kind":"meshWorkspace"}`, ""}, strings.Split(out.String(), "\n"))
}
