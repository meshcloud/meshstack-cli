package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/hal"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
)

type MeshObjectClient[M, Filter any] interface {
	// Read returns nil and no error where meshStack has no object with the id.
	Read(ctx context.Context, id string) (*M, error)
	List(ctx context.Context, filter Filter, options ...ListOption) iter.Seq2[M, error]
	Create(ctx context.Context, object M) (M, error)
	Update(ctx context.Context, object M) (M, error)
	// Delete takes a 404 as success, which a retried delete gets.
	Delete(ctx context.Context, id string) error
}

type NoFilter struct{}

type ListOption func(request *listOptions)

type listOptions struct {
	pageSize int
	until    time.Time
	page     *types.Page
}

func WithPageSize(size int) ListOption {
	return func(request *listOptions) {
		request.pageSize = size
	}
}

// WithNewestFirst yields to a filter's own sort and until.
func WithNewestFirst(until time.Time) ListOption {
	return func(request *listOptions) {
		request.until = until
	}
}

func WithCapturedPage(page *types.Page) ListOption {
	return func(request *listOptions) {
		request.page = page
	}
}

type meshObject interface {
	meshObjectId() string
}

type meshObjectAtPath interface {
	meshObjectPath() string
}

type meshObjectNoSortQuery interface {
	meshObjectNoSortQuery()
}

type meshObjectClient[M meshObject, Filter any] struct {
	client     http.AuthorizedClient
	collection *url.URL
	mediaType  string
	kind       string
	apiVersion string
}

func newMeshObjectClient[M meshObject, Filter any](ctx context.Context, client http.AuthorizedClient, endpoint xurl.URL, apiVersion string) meshObjectClient[M, Filter] {
	kind := inferKind[M]()
	path := strings.ToLower(pluralizeKind(kind))
	if atPath, ok := any(*new(M)).(meshObjectAtPath); ok {
		path = atPath.meshObjectPath()
	}
	c := meshObjectClient[M, Filter]{
		client:     client,
		collection: endpoint.JoinPath("api", "meshobjects", path),
		mediaType:  fmt.Sprintf("application/vnd.meshcloud.api.%s.%s.hal+json", kind, apiVersion),
		kind:       kind,
		apiVersion: apiVersion,
	}
	slog.DebugContext(ctx, fmt.Sprintf("initialized %s client", reflect.TypeFor[M]().Name()), "url", c.collection.String(), "kind", kind, "version", apiVersion)
	return c
}

func WithKindAndApiVersion[M, Filter any](c MeshObjectClient[M, Filter], object M) any {
	enveloper, ok := c.(interface{ envelope(object M) any })
	if !ok {
		panic(fmt.Sprintf("%T is no client that client.New built", c))
	}
	return enveloper.envelope(object)
}

var versionSuffixRe = regexp.MustCompile(`V\d+$`)

func inferKind[M any]() string {
	runes := []rune(reflect.TypeFor[M]().Name())
	runes[0] = unicode.ToLower(runes[0])
	return versionSuffixRe.ReplaceAllString(string(runes), "")
}

func pluralizeKind(kind string) string {
	return kind + "s"
}

func (c meshObjectClient[M, Filter]) do(ctx context.Context, method string, target *url.URL, options ...http.RequestOption) (io.ReadCloser, error) {
	return c.client.DoRequest(ctx, method, target, slices.Insert(options, 0, http.WithHeader("Accept", c.mediaType))...)
}

// followLink sends the bearer token only to the origin of the endpoint.
func (c meshObjectClient[M, Filter]) followLink(ctx context.Context, method, href string, options ...http.RequestOption) (io.ReadCloser, error) {
	target, err := c.collection.Parse(href)
	if err != nil {
		return nil, fmt.Errorf("meshStack sent the link %q, which is no URL: %w", href, err)
	}
	if target.Scheme != c.collection.Scheme || target.Host != c.collection.Host {
		return nil, fmt.Errorf("meshStack sent the link %s, which leaves the endpoint %s://%s", target.Redacted(), c.collection.Scheme, c.collection.Host)
	}
	return c.do(ctx, method, target, slices.Insert(options, 0, http.WithContentType(c.mediaType))...)
}

func (c meshObjectClient[M, Filter]) Read(ctx context.Context, id string) (*M, error) {
	object, err := c.unmarshalJsonBody[*M](c.do(ctx, http.MethodGet, c.collection.JoinPath(id)))
	if isNotFound(err) {
		//nolint:nilnil // a meshObject client returns nil on 404, which is how Terraform handles a resource that does not exist
		return nil, nil
	}
	return object, err
}

func (c meshObjectClient[M, Filter]) Create(ctx context.Context, object M) (M, error) {
	return c.unmarshalJsonBody[M](c.do(ctx, http.MethodPost, c.collection, http.WithJsonRequest(c.envelope(object), c.mediaType)))
}

func (c meshObjectClient[M, Filter]) Update(ctx context.Context, object M) (M, error) {
	return c.unmarshalJsonBody[M](c.do(ctx, http.MethodPut, c.collection.JoinPath(object.meshObjectId()),
		http.WithJsonRequest(c.envelope(object), c.mediaType), http.Retryable(http.RetryOnTransient)))
}

func (c meshObjectClient[M, Filter]) Delete(ctx context.Context, id string) error {
	err := http.IgnoreResponse(c.do(ctx, http.MethodDelete, c.collection.JoinPath(id)))
	if isNotFound(err) {
		return nil
	}
	return err
}

func (c meshObjectClient[M, Filter]) envelope(object M) any {
	return struct {
		ApiVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Object     M      `json:",embed"`
	}{c.apiVersion, c.kind, object}
}

func (c meshObjectClient[M, Filter]) List(ctx context.Context, filter Filter, options ...ListOption) iter.Seq2[M, error] {
	var opts listOptions
	for _, option := range options {
		option(&opts)
	}
	query := []http.RequestOption{http.WithUrlQuery(filter)}
	if !opts.until.IsZero() {
		// Without a fixed until, an object created during the listing pushes a read one onto the next page.
		newestFirst := url.Values{"until": {opts.until.UTC().Format(time.RFC3339Nano)}}
		if _, noSortQuery := any(*new(M)).(meshObjectNoSortQuery); !noSortQuery {
			newestFirst.Set("sort", "createdAt,desc")
		}
		query = []http.RequestOption{http.WithUrlQuery(newestFirst), http.WithUrlQuery(filter)}
	}
	return func(yield func(M, error) bool) {
		var noObject M
		embeddedKey := pluralizeKind(c.kind)
		for pageNumber := 0; ; pageNumber++ {
			page, err := c.page(ctx, pageNumber, opts.pageSize, query)
			if err != nil {
				yield(noObject, err)
				return
			}
			objects, ok := page.Embedded[embeddedKey]
			if !ok {
				yield(noObject, fmt.Errorf("embedded key %s not found in paginated response", embeddedKey))
				return
			}
			if opts.page != nil {
				*opts.page = page.Page
			}
			for _, object := range objects {
				if !yield(object, nil) {
					return
				}
			}
			if page.Page.Number >= page.Page.TotalPages-1 {
				return
			}
		}
	}
}

type paginatedResponse[M any] struct {
	Embedded map[string][]M `json:"_embedded"`
	Page     types.Page     `json:"page"`
}

func (c meshObjectClient[M, Filter]) page(ctx context.Context, pageNumber, pageSize int, query []http.RequestOption) (paginatedResponse[M], error) {
	pageQuery := map[string]any{"page": pageNumber}
	if pageSize > 0 {
		pageQuery["size"] = pageSize
	}
	page, err := c.unmarshalJsonBody[paginatedResponse[M]](c.do(ctx, http.MethodGet, c.collection, append(query, http.WithUrlQuery(pageQuery))...))
	if err != nil && pageNumber > 0 {
		// The first page fails for what fails the whole list, such as a missing role, and a page
		// number would only distract from it.
		return page, fmt.Errorf("cannot read page %d of the list: %w", pageNumber, err)
	}
	return page, err
}

func (c meshObjectClient[M, Filter]) unmarshalJsonBody[R any](body io.ReadCloser, err error) (R, error) {
	return http.UnmarshalJsonBody[R](body, err, hal.LinkUnmarshalers(c.followLink))
}

func isNotFound(err error) bool {
	httpErr, ok := errors.AsType[http.Error](err)
	return ok && httpErr.IsNotFound()
}

type MeshObjectByUuidMetadata struct {
	Uuid             *string `json:"uuid,omitzero" tfsdk:"uuid"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
}

func (m MeshObjectByUuidMetadata) meshObjectId() string {
	return *m.Uuid
}

type MeshObjectByNameMetadata struct {
	Name string `json:"name" tfsdk:"name"`
}

func (m MeshObjectByNameMetadata) meshObjectId() string {
	return m.Name
}
