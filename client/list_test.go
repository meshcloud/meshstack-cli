package client

import (
	"encoding/json/jsontext"
	"fmt"
	"iter"
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
	"github.com/meshcloud/meshstack-cli/internal/json"
)

const pageCount = 3

// inMemoryServerUrl is any URL the client accepts: the client of an [httptest.NewTestServer] sends
// every request to that server, and its own URL is http://example.com, which the client refuses
// for not being https.
var inMemoryServerUrl = xurl.MustParsef("http://localhost")

// newPagedWorkspaceClient serves pageCount pages of one workspace each, answering each page after
// servePage returns.
func newPagedWorkspaceClient(t *testing.T, servePage func(r *gohttp.Request, page int)) meshWorkspaceClient {
	t.Helper()
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		servePage(r, page)
		_, _ = fmt.Fprintf(w, `{"_embedded":{"meshWorkspaces":[{"metadata":{"name":"workspace-%d"}}]},"page":{"totalPages":%d,"number":%d}}`,
			page, pageCount, page)
	}))
	return newWorkspaceClient(t.Context(), internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(http.BearerToken("token")),
		EndpointUrl:      inMemoryServerUrl,
	})
}

func collect(t *testing.T, items iter.Seq2[jsontext.Value, error]) (names []string, err error) {
	t.Helper()
	for item, itemErr := range items {
		if itemErr != nil {
			return names, itemErr
		}
		var workspace MeshWorkspace
		require.NoError(t, json.Unmarshal(item, &workspace))
		names = append(names, workspace.Metadata.Name)
	}
	return names, nil
}

// The Terraform provider sets no list options, and must get no page size it did not ask for.
func TestWithoutListOptionsAPageCarriesNoSize(t *testing.T) {
	var sizesAskedFor []string
	workspaceClient := newPagedWorkspaceClient(t, func(r *gohttp.Request, _ int) {
		sizesAskedFor = append(sizesAskedFor, r.URL.Query().Get("size"))
	})

	_, err := collect(t, workspaceClient.ListRawSeq(t.Context()))

	require.NoError(t, err)
	assert.Equal(t, []string{"", "", ""}, sizesAskedFor)
}

// meshStack caps the page size it is asked for, so pages smaller than asked must lose no items.
func TestPageSizeAsksForPagesOfThatSizeAndTakesSmallerOnes(t *testing.T) {
	const serverCap, askedFor = 2, 3
	names := []string{"workspace-0", "workspace-1", "workspace-2", "workspace-3", "workspace-4"}
	var sizesAskedFor []string
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		sizesAskedFor = append(sizesAskedFor, r.URL.Query().Get("size"))
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		var items []string
		for _, name := range names[min(page*serverCap, len(names)):min((page+1)*serverCap, len(names))] {
			items = append(items, fmt.Sprintf(`{"metadata":{"name":%q}}`, name))
		}
		totalPages := (len(names) + serverCap - 1) / serverCap
		_, _ = fmt.Fprintf(w, `{"_embedded":{"meshWorkspaces":[%s]},"page":{"size":%d,"totalPages":%d,"number":%d}}`,
			strings.Join(items, ","), serverCap, totalPages, page)
	}))
	workspaceClient := newWorkspaceClient(t.Context(), internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(http.BearerToken("token")),
		EndpointUrl:      inMemoryServerUrl,
	})

	got, err := collect(t, workspaceClient.ListRawSeq(WithListOptions(t.Context(), ListOptions{PageSize: askedFor})))

	require.NoError(t, err)
	assert.Equal(t, names, got)
	assert.Equal(t, []string{"3", "3", "3"}, sizesAskedFor)
}

func TestOnPageSeesEachPageBeforeItsItems(t *testing.T) {
	workspaceClient := newPagedWorkspaceClient(t, func(*gohttp.Request, int) {})
	var events []string
	ctx := WithListOptions(t.Context(), ListOptions{OnPage: func(page Page) {
		events = append(events, fmt.Sprintf("page %d of %d", page.Number, page.TotalPages))
	}})

	for item, err := range workspaceClient.ListRawSeq(ctx) {
		require.NoError(t, err)
		var workspace MeshWorkspace
		require.NoError(t, json.Unmarshal(item, &workspace))
		events = append(events, workspace.Metadata.Name)
	}

	assert.Equal(t, []string{
		"page 0 of 3", "workspace-0",
		"page 1 of 3", "workspace-1",
		"page 2 of 3", "workspace-2",
	}, events)
}
