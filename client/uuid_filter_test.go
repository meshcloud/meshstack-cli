package client

import (
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestAUuidOfAFilterGoesAsItsTextAndAZeroOneNotAtAll(t *testing.T) {
	server := fakemeshstack.Start(t, fakemeshstack.Options{})
	httpClient := newTestHttpClient(server)
	raw := newRawClient(httpClient).with(newBuildingBlockRunClient(t.Context(), httpClient).meshObject)

	for _, filter := range []MeshBuildingBlockRunListFilter{
		{BuildingBlockUuid: uuid.MustParse("0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90")},
		{},
	} {
		for _, err := range raw.List[MeshBuildingBlockRun](t.Context(), filter, ListOptions{}) {
			require.NoError(t, err)
		}
	}

	requests := server.TakeRequests()
	require.Len(t, requests, 2)
	assert.Equal(t, []string{"0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90"}, requests[0].URL.Query()["buildingBlockUuid"])
	assert.NotContains(t, requests[1].URL.Query(), "buildingBlockUuid")
}

func TestListingTheVersionsOfADefinitionRejectsAnIdOfNoUuidBeforeItAsksMeshStack(t *testing.T) {
	server := fakemeshstack.Start(t, fakemeshstack.Options{})
	versions := newBuildingBlockDefinitionVersionClient(t.Context(), newTestHttpClient(server))

	_, err := versions.List(t.Context(), "my-definition")

	require.EqualError(t, err, `building block definition "my-definition": invalid uuid`)
	assert.Empty(t, server.TakeRequests())
}
