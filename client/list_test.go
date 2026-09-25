package client

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	gohttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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
// servePage returns, and records for each page request whether the client sent it with a deadline.
// The server is in memory, so a test can run it inside a [synctest.Test] bubble.
func newPagedWorkspaceClient(t *testing.T, servePage func(r *gohttp.Request, page int)) (_ meshWorkspaceClient, sentWithDeadline *[]bool) {
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
	authorization := &deadlineRecordingToken{BearerToken: "token"}
	return newWorkspaceClient(t.Context(), internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(authorization),
		EndpointUrl:      inMemoryServerUrl,
	}), &authorization.sentWithDeadline
}

// deadlineRecordingToken sees the context of every request, because the token is fetched with it.
type deadlineRecordingToken struct {
	http.BearerToken

	sentWithDeadline []bool
}

func (a *deadlineRecordingToken) GetBearerToken(ctx context.Context) (http.BearerToken, error) {
	_, hasDeadline := ctx.Deadline()
	a.sentWithDeadline = append(a.sentWithDeadline, hasDeadline)
	return a.BearerToken, nil
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

func TestPageTimeoutBoundsEachPageRatherThanTheListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const pageTimeout = 45 * time.Second
		workspaceClient, sentWithDeadline := newPagedWorkspaceClient(t, func(*gohttp.Request, int) {
			synctest.Sleep(pageTimeout / 2)
		})
		start := time.Now()

		names, err := collect(t, workspaceClient.ListRawSeq(WithListOptions(t.Context(), ListOptions{PageTimeout: pageTimeout})))

		require.NoError(t, err, "every page arrives within the timeout, though all of them together take longer")
		assert.Equal(t, pageCount*pageTimeout/2, time.Since(start))
		assert.Equal(t, []string{"workspace-0", "workspace-1", "workspace-2"}, names)
		assert.Equal(t, []bool{true, true, true}, *sentWithDeadline)
	})
}

func TestPageTimeoutGivesUpOnAPageThatDoesNotArrive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const pageTimeout = 45 * time.Second
		workspaceClient, _ := newPagedWorkspaceClient(t, func(r *gohttp.Request, page int) {
			if page == 1 {
				<-r.Context().Done()
			}
		})
		start := time.Now()

		names, err := collect(t, workspaceClient.ListRawSeq(WithListOptions(t.Context(), ListOptions{PageTimeout: pageTimeout})))

		assert.Equal(t, pageTimeout, time.Since(start))
		assert.Equal(t, []string{"workspace-0"}, names)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorContains(t, err, "page 1 did not arrive within 45s")
	})
}

// The Terraform provider sets no list options, and must get no page size or timeout it did not ask
// for.
func TestWithoutListOptionsAPageCarriesNoSizeAndNoDeadline(t *testing.T) {
	var sizesAskedFor []string
	workspaceClient, sentWithDeadline := newPagedWorkspaceClient(t, func(r *gohttp.Request, _ int) {
		sizesAskedFor = append(sizesAskedFor, r.URL.Query().Get("size"))
	})

	_, err := collect(t, workspaceClient.ListRawSeq(t.Context()))

	require.NoError(t, err)
	assert.Equal(t, []string{"", "", ""}, sizesAskedFor)
	assert.Equal(t, []bool{false, false, false}, *sentWithDeadline)
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
	workspaceClient, _ := newPagedWorkspaceClient(t, func(*gohttp.Request, int) {})
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
