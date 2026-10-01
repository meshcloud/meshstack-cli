package client

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"iter"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

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

func newTestHttpClient(server *httptest.Server) internal.HttpClient {
	return internal.HttpClient{
		AuthorizedClient: http.Client{Client: server.Client(), UserAgent: "test-agent"}.WithAuthorization(http.BearerToken("token")),
		EndpointUrl:      inMemoryServerUrl,
	}
}

func newTestRawWorkspaceClient(t *testing.T, handler gohttp.HandlerFunc) *RawClient {
	t.Helper()
	httpClient := newTestHttpClient(httptest.NewTestServer(t, handler))
	return newRawClient(httpClient).with(newWorkspaceClient(t.Context(), httpClient).meshObject)
}

// newPagedWorkspaceClient serves pageCount pages of one workspace each, answering each page after
// servePage returns.
func newPagedWorkspaceClient(t *testing.T, servePage func(r *gohttp.Request, page int)) *RawClient {
	t.Helper()
	return newTestRawWorkspaceClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		servePage(r, page)
		_, _ = fmt.Fprintf(w, `{"_embedded":{"meshWorkspaces":[{"metadata":{"name":"workspace-%d"}}]},"page":{"totalPages":%d,"number":%d}}`,
			page, pageCount, page)
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
	raw := newPagedWorkspaceClient(t, func(r *gohttp.Request, _ int) {
		sizesAskedFor = append(sizesAskedFor, r.URL.Query().Get("size"))
	})

	_, err := collect(t, raw.List[MeshWorkspace](t.Context(), MeshWorkspaceListFilter{}, ListOptions{}))

	require.NoError(t, err)
	assert.Equal(t, []string{"", "", ""}, sizesAskedFor)
}

// meshStack caps the page size it is asked for, so pages smaller than asked must lose no items.
func TestPageSizeAsksForPagesOfThatSizeAndTakesSmallerOnes(t *testing.T) {
	const serverCap, askedFor = 2, 3
	names := []string{"workspace-0", "workspace-1", "workspace-2", "workspace-3", "workspace-4"}
	var sizesAskedFor []string
	raw := newTestRawWorkspaceClient(t, func(w gohttp.ResponseWriter, r *gohttp.Request) {
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
	})

	got, err := collect(t, raw.List[MeshWorkspace](t.Context(), MeshWorkspaceListFilter{}, ListOptions{PageSize: askedFor}))

	require.NoError(t, err)
	assert.Equal(t, names, got)
	assert.Equal(t, []string{"3", "3", "3"}, sizesAskedFor)
}

func TestAListIsNewestFirstAndEndsWhenItStarts(t *testing.T) {
	var sorts, untils []string
	raw := newPagedWorkspaceClient(t, func(r *gohttp.Request, _ int) {
		sorts = append(sorts, r.URL.Query().Get("sort"))
		untils = append(untils, r.URL.Query().Get("until"))
	})
	before := time.Now()

	_, err := collect(t, raw.List[MeshWorkspace](t.Context(), MeshWorkspaceListFilter{}, ListOptions{}))

	require.NoError(t, err)
	assert.Equal(t, slices.Repeat([]string{"createdAt,desc"}, pageCount), sorts)
	assert.Equal(t, slices.Repeat(untils[:1], pageCount), untils, "every page ends at the same instant")
	until, err := time.Parse(time.RFC3339Nano, untils[0])
	require.NoError(t, err)
	assert.WithinRange(t, until, before, time.Now())
}

func TestAListSendsTheFilterAsQueryParameters(t *testing.T) {
	var sorts [][]string
	raw := newPagedWorkspaceClient(t, func(r *gohttp.Request, _ int) {
		sorts = append(sorts, r.URL.Query()["sort"])
	})

	_, err := collect(t, raw.List[MeshWorkspace](t.Context(), MeshWorkspaceListFilter{Sort: []string{"createdAt,desc", "name,asc"}}, ListOptions{}))

	require.NoError(t, err)
	assert.Equal(t, slices.Repeat([][]string{{"createdAt,desc", "name,asc"}}, pageCount), sorts)
}

func TestAKindWithoutATypedClientFailsToList(t *testing.T) {
	raw := newPagedWorkspaceClient(t, func(*gohttp.Request, int) {
		t.Error("the raw client asked meshStack for a kind it has no API for")
	})

	_, err := collect(t, raw.List[MeshTenant](t.Context(), nil, ListOptions{}))

	require.ErrorContains(t, err, "client.MeshTenant")
}

type rawRequest struct {
	method, host, pathAndQuery, accept, authorization, body string
}

func newRecordingRawClient(t *testing.T, status int, answer string) (*RawClient, *[]rawRequest) {
	t.Helper()
	var requests []rawRequest
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests = append(requests, rawRequest{
			method:        r.Method,
			host:          r.Host,
			pathAndQuery:  r.URL.RequestURI(),
			accept:        r.Header.Get("Accept"),
			authorization: r.Header.Get("Authorization"),
			body:          string(body),
		})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	httpClient := newTestHttpClient(server)
	return newRawClient(httpClient).with(newBuildingBlockRunClient(t.Context(), httpClient).meshObject), &requests
}

func TestGetReadsTheSubResourceOfAnObjectInItsKindsMediaType(t *testing.T) {
	raw, requests := newRecordingRawClient(t, gohttp.StatusOK, `{"steps": []}`)
	runUuid := uuid.MustParse("b1d2c3e4-0000-4000-8000-000000000001")

	logs, err := raw.Get[MeshBuildingBlockRun](t.Context(), runUuid, "logs")

	require.NoError(t, err)
	assert.JSONEq(t, `{"steps": []}`, string(logs))
	assert.Equal(t, []rawRequest{{
		method:        gohttp.MethodGet,
		host:          inMemoryServerUrl.Host,
		pathAndQuery:  "/api/meshobjects/meshbuildingblockruns/b1d2c3e4-0000-4000-8000-000000000001/logs",
		accept:        "application/vnd.meshcloud.api.meshBuildingBlockRun.v1.hal+json",
		authorization: "Bearer token",
	}}, *requests)
}

func TestDoRequestSendsTheRequestToThePathAndReturnsTheAnswerAsItCame(t *testing.T) {
	raw, requests := newRecordingRawClient(t, gohttp.StatusOK, `{"answer": 1}`)

	answer, err := raw.DoRequest(t.Context(), gohttp.MethodPost, "/api/x",
		http.WithUrlQuery(url.Values{"dry": {"true"}, "tag": {"a", "b"}}),
		http.WithAccept("application/vnd.custom+json"),
		http.WithBody([]byte(`{"spec":{}}`)))

	require.NoError(t, err)
	assert.Equal(t, `{"answer": 1}`, string(answer))
	assert.Equal(t, []rawRequest{{
		method:        gohttp.MethodPost,
		host:          inMemoryServerUrl.Host,
		pathAndQuery:  "/api/x?dry=true&tag=a&tag=b",
		accept:        "application/vnd.custom+json",
		authorization: "Bearer token",
		body:          `{"spec":{}}`,
	}}, *requests)
}

func TestDoRequestReturnsTheBodyOfAFailedAnswerInTheError(t *testing.T) {
	raw, _ := newRecordingRawClient(t, gohttp.StatusConflict, `{"message":"still in use"}`)

	answer, err := raw.DoRequest(t.Context(), gohttp.MethodDelete, "/api/x")

	assert.Nil(t, answer)
	httpErr, ok := errors.AsType[HttpError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, gohttp.StatusConflict, httpErr.StatusCode)
	assert.JSONEq(t, `{"message":"still in use"}`, string(httpErr.ResponseBody))
}

func TestDoRequestSendsAnyPathToTheEndpointAlone(t *testing.T) {
	raw, requests := newRecordingRawClient(t, gohttp.StatusOK, `{}`)

	for _, path := range []string{"https://other.host/api/x", "//other.host/api/x"} {
		_, err := raw.DoRequest(t.Context(), gohttp.MethodGet, path)

		require.NoError(t, err)
	}
	require.Len(t, *requests, 2)
	for _, request := range *requests {
		assert.Equal(t, inMemoryServerUrl.Host, request.host, "the path is joined onto the endpoint, so the token goes nowhere else")
	}
}
