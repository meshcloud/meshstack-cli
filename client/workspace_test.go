package client

import (
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Decoding into MeshWorkspace and encoding again would drop what the model lacks: apiVersion and
// kind, which a POST of the edited object needs, and _links, which shows an LLM agent how
// meshObjects relate.
func TestARawListYieldsEveryWorkspaceOfEveryPageAsSent(t *testing.T) {
	platformTeam := `
	{
		"kind": "meshWorkspace",
		"apiVersion": "v2",
		"metadata": {"name": "platform-team", "tags": {}, "createdOn": "2017-12-22T10:37:42Z"},
		"spec": {"displayName": "Platform Team", "platformBuilderAccessEnabled": true},
		"_links": {
			"self": {"href": "http://localhost:8080/api/meshobjects/meshworkspaces/platform-team"}
		}
	}`
	appTeam := `
	{
		"kind": "meshWorkspace",
		"apiVersion": "v2",
		"metadata": {"name": "app-team", "tags": {}, "createdOn": "2017-12-22T10:37:43Z"},
		"spec": {"displayName": "App Team", "platformBuilderAccessEnabled": false},
		"_links": {
			"self": {"href": "http://localhost:8080/api/meshobjects/meshworkspaces/app-team"}
		}
	}`
	workspaces := []string{platformTeam, appTeam}
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.Equal(t, "/api/meshobjects/meshworkspaces", r.URL.Path) || !assert.NoError(t, err) || !assert.Less(t, page, len(workspaces)) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{"_embedded":{"meshWorkspaces":[%s]},"page":{"totalPages":%d,"number":%d}}`, workspaces[page], len(workspaces), page)
	}))
	httpClient := newTestHttpClient(server)
	raw := newRawClient(httpClient).with(newWorkspaceClient(t.Context(), httpClient).meshObject)

	var got []string
	for workspace, err := range raw.List[MeshWorkspace](t.Context(), MeshWorkspaceListFilter{}, ListOptions{}) {
		require.NoError(t, err)
		got = append(got, string(workspace))
	}

	// The whitespace around a value is the page's, not the item's, so only that is trimmed.
	assert.Equal(t, []string{strings.TrimSpace(platformTeam), strings.TrimSpace(appTeam)}, got)
}
