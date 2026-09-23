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

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// This is a sort-of regression test, because a previous implementation deserialized the backend response into MeshWorkspace
// and then serialized it to JSON again before printing it, which turned out to be a bad idea:
// The MeshWorkspace model does not include some properties that the raw JSON sent by the backend does include, such as
// `apiVersion`, `kind` and `_links`. But these properties shouldn't be swallowed. `apiVersion` and `kind` are a
// mandatory when POSTing meshObjects, so imagine a scenario where the CLI is used to fetch entities, modify them, and
// then send them back to the server. This would then fail. `_links` should also not be swallowed, because it's useful for LLM
// agents to understand relations between meshObjects.
func TestListRawSeqYieldsEveryWorkspaceOfEveryPageAsSent(t *testing.T) {
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
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.Equal(t, "/api/meshobjects/meshworkspaces", r.URL.Path) || !assert.NoError(t, err) || !assert.Less(t, page, len(workspaces)) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{"_embedded":{"meshWorkspaces":[%s]},"page":{"totalPages":%d,"number":%d}}`, workspaces[page], len(workspaces), page)
	}))
	t.Cleanup(server.Close)
	workspaceClient := newWorkspaceClient(t.Context(), internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(http.BearerToken("token")),
		EndpointUrl:      xurl.MustParsef("%s", server.URL),
	})

	var got []string
	for workspace, err := range workspaceClient.ListRawSeq(t.Context()) {
		require.NoError(t, err)
		got = append(got, string(workspace))
	}

	// The whitespace around a value is the page's, not the item's, so only that is trimmed.
	assert.Equal(t, []string{strings.TrimSpace(platformTeam), strings.TrimSpace(appTeam)}, got)
}
