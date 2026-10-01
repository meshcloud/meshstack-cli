package client

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"net/url"
	"reflect"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type ListOptions = internal.ListOptions

type Page = internal.Page

// SortCriteria holds one `property,(asc|desc)` criterion per element, such as createdAt,desc.
type SortCriteria []string

// RawClient answers with the JSON meshStack sent, for the CLI, which writes it out as it came. It
// is the CLI's alone: DoRequest takes the options of internal/http, which Go's internal rule closes
// to the Terraform provider.
type RawClient struct {
	httpClient internal.HttpClient
	// apis is keyed by the Go type rather than the kind, because MeshBuildingBlock and
	// MeshBuildingBlockV2 share a kind in different API versions.
	apis map[reflect.Type]internal.MeshObjectApi
}

func newRawClient(httpClient internal.HttpClient) *RawClient {
	return &RawClient{httpClient: httpClient, apis: map[reflect.Type]internal.MeshObjectApi{}}
}

// List pages through the objects of M, narrowed by filter, a struct whose fields are the query
// parameters of the listing.
func (r *RawClient) List[M any](ctx context.Context, filter any, options ListOptions) iter.Seq2[jsontext.Value, error] {
	api, err := r.api[M]()
	if err != nil {
		return func(yield func(jsontext.Value, error) bool) {
			yield(nil, err)
		}
	}
	// Newest first, an object created while the pages are read would land on a page already read
	// and push another one onto the next page, to be listed twice. The fixed until leaves it out; an
	// endpoint without until ignores the parameter. The filter's own sort and until win.
	newestFirst := url.Values{"sort": {"createdAt,desc"}, "until": {time.Now().UTC().Format(time.RFC3339Nano)}}
	return api.ListSeqAs[jsontext.Value](ctx, options, http.WithUrlQuery(newestFirst), http.WithUrlQuery(filter))
}

// Get reads the object of M with the uuid, or the sub-resource that pathElems name below it.
func (r *RawClient) Get[M any](ctx context.Context, uuid uuid.UUID, pathElems ...string) (jsontext.Value, error) {
	api, err := r.api[M]()
	if err != nil {
		return nil, err
	}
	return api.DoRequest[jsontext.Value](ctx, http.MethodGet, api.ApiUrl.JoinPath(uuid.String()).JoinPath(pathElems...),
		http.WithAccept(api.MeshObjectMimeType()))
}

// DoRequest joins path onto the endpoint, even one that names a host, so that the bearer token
// never leaves for another host. The answer is of any content type, such as a plan artifact's
// application/octet-stream, so it is returned unparsed.
func (r *RawClient) DoRequest(ctx context.Context, method, path string, opts ...http.RequestOption) ([]byte, error) {
	return r.httpClient.DoRequest[[]byte](ctx, method, r.httpClient.EndpointUrl.JoinPath(path), opts...)
}

// with makes List and Get reach the objects of M through the API version of meshObject, so that
// the version stays declared once, in the constructor of M's typed client.
func (r *RawClient) with[M any](meshObject internal.MeshObjectClient[M]) *RawClient {
	r.apis[reflect.TypeFor[M]()] = meshObject.MeshObjectApi
	return r
}

func (r *RawClient) api[M any]() (internal.MeshObjectApi, error) {
	api, ok := r.apis[reflect.TypeFor[M]()]
	if !ok {
		return api, fmt.Errorf("the raw client has no API for %s: add its typed client in client.New", reflect.TypeFor[M]())
	}
	return api, nil
}
