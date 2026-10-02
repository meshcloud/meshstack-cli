package client

import (
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// inMemoryServerUrl is any URL the client accepts: the client of an [httptest.NewTestServer] sends
// every request to that server, and its own URL is http://example.com, which the client refuses
// for not being https.
var inMemoryServerUrl = xurl.MustParsef("http://localhost")

func newTestHttpClient(server *httptest.Server) internal.HttpClient {
	return internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(http.BearerToken("token")),
		EndpointUrl:      inMemoryServerUrl,
	}
}

func TestRawClient(t *testing.T) {
	const serverPageSize = 2
	var workspaces []string
	for i := range 5 {
		workspaces = append(workspaces, fmt.Sprintf(`{"kind": "meshWorkspace", "apiVersion": "v2", "metadata": {"name": "workspace-%d"}, `+
			`"_links": {"self": {"href": "http://localhost:8080/api/meshobjects/meshworkspaces/workspace-%d"}}}`, i, i))
	}
	var requests []*gohttp.Request
	httpClient := newTestHttpClient(httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		requests = append(requests, r)
		if r.URL.Path != "/api/meshobjects/meshworkspaces" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		onPage := workspaces[min(page*serverPageSize, len(workspaces)):min((page+1)*serverPageSize, len(workspaces))]
		totalPages := (len(workspaces) + serverPageSize - 1) / serverPageSize
		_, _ = fmt.Fprintf(w, `{ "_embedded": { "meshWorkspaces": [ %s ] }, "page": { "size": %d, "totalPages": %d, "number": %d } }`,
			strings.Join(onPage, ", "), serverPageSize, totalPages, page)
	})))
	raw := newRawClient(httpClient).with(newWorkspaceClient(t.Context(), httpClient).meshObject)
	list := func(t *testing.T, filter MeshWorkspaceListFilter, options ListOptions) (listed []string) {
		t.Helper()
		requests = nil
		for item, err := range raw.List[MeshWorkspace](t.Context(), filter, options) {
			require.NoError(t, err)
			listed = append(listed, string(item))
		}
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
		requests = nil
		var failed error
		for _, err := range raw.List[MeshTenant](t.Context(), nil, ListOptions{}) {
			failed = err
		}
		require.ErrorContains(t, failed, "client.MeshTenant")
		assert.Empty(t, requests)
	})

	t.Run("DoRequest joins any path onto the endpoint, so the token goes nowhere else", func(t *testing.T) {
		requests = nil
		for _, path := range []string{"https://other.host/api/x", "//other.host/api/x"} {
			_, err := raw.DoRequest(t.Context(), gohttp.MethodGet, path)
			require.NoError(t, err)
		}
		require.Len(t, requests, 2)
		for _, r := range requests {
			assert.Equal(t, inMemoryServerUrl.Host, r.Host)
		}
	})
}
