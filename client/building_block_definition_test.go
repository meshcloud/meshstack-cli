package client

import (
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADefinitionListingSendsItsWorkspaceAndAllPublished(t *testing.T) {
	var query url.Values
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		query = r.URL.Query()
		_, _ = io.WriteString(w, `{"_embedded": {"meshBuildingBlockDefinitions": []}, "page": {"totalPages": 1, "number": 0}}`)
	}))
	definitions := newBuildingBlockDefinitionClient(t.Context(), newTestHttpClient(server))

	_, err := definitions.List(t.Context(), new("ops"))

	require.NoError(t, err)
	assert.Equal(t, url.Values{
		"ownedByWorkspace":    {"ops"},
		"includeAllPublished": {"true"},
		"page":                {"0"},
	}, query)
}
