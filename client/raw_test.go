package client

import (
	"encoding/json/jsontext"
	"fmt"
	"io"
	gohttp "net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func newTestHttpClient(server *fakemeshstack.Server) internal.HttpClient {
	return internal.HttpClient{
		AuthorizedClient: http.NewClient("test-agent").WithAuthorization(http.BearerToken(fakemeshstack.Token)),
		EndpointUrl:      xurl.MustParsef("%s", server.URL),
	}
}

func TestRawClient(t *testing.T) {
	const serverPageSize = 2
	var workspaces []string
	for i := range 5 {
		workspaces = append(workspaces, fmt.Sprintf(`{"kind":"meshWorkspace","apiVersion":"v2","metadata":{"name":"workspace-%d"},`+
			`"_links":{"self":{"href":"http://localhost:8080/api/meshobjects/meshworkspaces/workspace-%d"}}}`, i, i))
	}
	var items []any
	for _, workspace := range workspaces {
		items = append(items, jsontext.Value(workspace))
	}
	server := fakemeshstack.Start(t, fakemeshstack.Options{Workspaces: items, PageSize: serverPageSize})
	httpClient := newTestHttpClient(server)
	raw := newRawClient(httpClient).with(newWorkspaceClient(t.Context(), httpClient).meshObject)
	var requests []fakemeshstack.Request
	list := func(t *testing.T, filter MeshWorkspaceListFilter, options ListOptions) (listed []string) {
		t.Helper()
		server.TakeRequests()
		for item, err := range raw.List[MeshWorkspace](t.Context(), filter, options) {
			require.NoError(t, err)
			listed = append(listed, string(item))
		}
		requests = server.TakeRequests()
		return listed
	}
	asked := func(parameter string) (values []string) {
		for _, r := range requests {
			values = append(values, strings.Join(r.URL.Query()[parameter], " "))
		}
		return values
	}

	before := time.Now()
	// Decoding into MeshWorkspace and encoding again would drop what the model lacks: apiVersion and
	// kind, which a POST of the edited object needs, and _links, which shows an LLM agent how
	// meshObjects relate.
	t.Run("a list yields every item of every page as meshStack sent it", func(t *testing.T) {
		assert.Equal(t, workspaces, list(t, MeshWorkspaceListFilter{}, ListOptions{}))
	})

	t.Run("the list asks for newest first, and every page ends at the instant the list started", func(t *testing.T) {
		assert.Equal(t, slices.Repeat([]string{"createdAt,desc"}, 3), asked("sort"))
		untils := asked("until")
		assert.Equal(t, slices.Repeat(untils[:1], 3), untils)
		until, err := time.Parse(time.RFC3339Nano, untils[0])
		require.NoError(t, err)
		assert.WithinRange(t, until, before, time.Now())
	})

	// The Terraform provider sets no list options, and must get no page size it did not ask for.
	t.Run("without list options a page carries no size", func(t *testing.T) {
		assert.Equal(t, []string{"", "", ""}, asked("size"))
	})

	t.Run("a page size asks for pages of that size, and takes the smaller ones meshStack caps it to", func(t *testing.T) {
		assert.Equal(t, workspaces, list(t, MeshWorkspaceListFilter{}, ListOptions{PageSize: 3}))
		assert.Equal(t, []string{"3", "3", "3"}, asked("size"))
	})

	t.Run("the sort of a filter replaces newest first", func(t *testing.T) {
		list(t, MeshWorkspaceListFilter{Sort: []string{"createdAt,desc", "name,asc"}}, ListOptions{})
		assert.Equal(t, slices.Repeat([]string{"createdAt,desc name,asc"}, 3), asked("sort"))
	})

	t.Run("a kind without a typed client fails before it asks meshStack", func(t *testing.T) {
		server.TakeRequests()
		var failed error
		for _, err := range raw.List[MeshTenant](t.Context(), nil, ListOptions{}) {
			failed = err
		}
		require.ErrorContains(t, failed, "client.MeshTenant")
		assert.Empty(t, server.TakeRequests())
	})

	t.Run("DoRequest joins any path onto the endpoint, so the token goes nowhere else", func(t *testing.T) {
		server.Route("/", func(w gohttp.ResponseWriter, _ *gohttp.Request) { _, _ = io.WriteString(w, `{}`) })
		server.TakeRequests()
		for _, path := range []string{"https://other.host/api/x", "//other.host/api/x"} {
			_, err := raw.DoRequest(t.Context(), gohttp.MethodGet, path)
			require.NoError(t, err)
		}
		requests := server.TakeRequests()
		require.Len(t, requests, 2)
		for _, r := range requests {
			assert.Equal(t, httpClient.EndpointUrl.Host, r.Host)
		}
	})
}
