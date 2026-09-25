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

func TestJsonWritesOneArrayOfTheItemsAsTheServerSentThem(t *testing.T) {
	platformTeam := `
	{
		"kind": "meshWorkspace",
		"apiVersion": "v2",
		"metadata": {"name": "platform-team", "deletedOn": null},
		"spec": {"displayName": "\u001b[31mPlatform Team", "tags": {"budget": [18446744073709551616, 1.5e300, ".inf"]}},
		"_links": {
			"self": {"href": "http://localhost:8080/api/meshobjects/meshworkspaces/platform-team"}
		}
	}`
	appTeam := `{"kind": "meshWorkspace", "apiVersion": "v2", "metadata": {"name": "app-team"}}`
	var out strings.Builder

	err := internal.WriteList(&out, internal.OutputJson, items(platformTeam, appTeam))

	require.NoError(t, err)
	want := strings.TrimPrefix(`
[
  {
    "kind": "meshWorkspace",
    "apiVersion": "v2",
    "metadata": {
      "name": "platform-team",
      "deletedOn": null
    },
    "spec": {
      "displayName": "\u001b[31mPlatform Team",
      "tags": {
        "budget": [
          18446744073709551616,
          1.5e300,
          ".inf"
        ]
      }
    },
    "_links": {
      "self": {
        "href": "http://localhost:8080/api/meshobjects/meshworkspaces/platform-team"
      }
    }
  },
  {
    "kind": "meshWorkspace",
    "apiVersion": "v2",
    "metadata": {
      "name": "app-team"
    }
  }
]
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

func TestAnEmptyListing(t *testing.T) {
	for format, want := range map[internal.OutputFormat]string{
		internal.OutputJson:   "[]\n",
		internal.OutputNdjson: "",
	} {
		t.Run(string(format), func(t *testing.T) {
			var out strings.Builder

			err := internal.WriteList(&out, format, items())

			require.NoError(t, err)
			assert.Equal(t, want, out.String())
		})
	}
}

func TestAFailingPageStopsTheListing(t *testing.T) {
	pageErr := errors.New("page 1 failed")
	failing := func(yield func(jsontext.Value, error) bool) {
		if yield(jsontext.Value(`{"kind":"meshWorkspace"}`), nil) {
			yield(nil, pageErr)
		}
	}
	for format, want := range map[internal.OutputFormat][]string{
		internal.OutputJson:   {"[", "  {", `    "kind": "meshWorkspace"`, "  }"},
		internal.OutputNdjson: {`{"kind":"meshWorkspace"}`, ""},
	} {
		t.Run(string(format), func(t *testing.T) {
			var out strings.Builder

			err := internal.WriteList(&out, format, failing)

			require.ErrorIs(t, err, pageErr)
			assert.Equal(t, want, strings.Split(out.String(), "\n"))
			if format == internal.OutputJson {
				assert.False(t, jsontext.Value(out.String()).IsValid(), "a listing cut short by an error must not parse as complete")
			}
		})
	}
}
