package client

import (
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAUuidOfAFilterGoesAsItsTextAndAZeroOneNotAtAll(t *testing.T) {
	var queries []url.Values
	httpClient := newTestHttpClient(httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		queries = append(queries, r.URL.Query())
		_, _ = io.WriteString(w, `{"_embedded": {"meshBuildingBlockRuns": []}, "page": {"totalPages": 1, "number": 0}}`)
	})))
	raw := newRawClient(httpClient).with(newBuildingBlockRunClient(t.Context(), httpClient).meshObject)

	for _, filter := range []MeshBuildingBlockRunListFilter{
		{BuildingBlockUuid: uuid.MustParse("0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90")},
		{},
	} {
		for _, err := range raw.List[MeshBuildingBlockRun](t.Context(), filter, ListOptions{}) {
			require.NoError(t, err)
		}
	}

	require.Len(t, queries, 2)
	assert.Equal(t, []string{"0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90"}, queries[0]["buildingBlockUuid"])
	assert.NotContains(t, queries[1], "buildingBlockUuid")
}

func TestListingTheVersionsOfADefinitionRejectsAnIdOfNoUuidBeforeItAsksMeshStack(t *testing.T) {
	versions := newBuildingBlockDefinitionVersionClient(t.Context(), newTestHttpClient(httptest.NewTestServer(t, gohttp.HandlerFunc(func(gohttp.ResponseWriter, *gohttp.Request) {
		t.Error("the client asked meshStack for the versions of a definition of no uuid")
	}))))

	_, err := versions.List(t.Context(), "my-definition")

	assert.EqualError(t, err, `building block definition "my-definition": invalid uuid`)
}
