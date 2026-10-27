// Package meshapi sends the requests of the clients that client/openapi/generator writes. It is
// public so that another module can generate clients of its own.
package meshapi

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"reflect"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	internaljson "github.com/meshcloud/meshstack-cli/internal/json"
)

// HttpClient sends requests to one meshStack, authorized.
type HttpClient struct {
	http.AuthorizedClient

	EndpointUrl xurl.URL
}

// Api is one version of a meshObject kind as the generated clients call it.
type Api struct {
	HttpClient

	Kind       string
	ApiVersion string
	MediaType  string
	// Path is that of the kind's collection, such as /api/meshobjects/meshworkspaces.
	Path string
	// EmbeddedRel names the objects in a page of the listing.
	EmbeddedRel string
}

// Call is one request of a generated client.
type Call struct {
	Method string
	Path   string
	Query  any
	// SendsMeshObject and AnswersMeshObject mark a body and an answer that carry the kind and
	// apiVersion, which the generated types leave out.
	SendsMeshObject   bool
	AnswersMeshObject bool
}

// Path fills the parameters of the template in their order.
func Path(template string, values ...string) string {
	var path strings.Builder
	rest := template
	for _, value := range values {
		before, after, ok := strings.Cut(rest, "{")
		_, after, closed := strings.Cut(after, "}")
		if !ok || !closed {
			panic(fmt.Sprintf("%s has fewer parameters than %d", template, len(values)))
		}
		path.WriteString(before + url.PathEscape(value))
		rest = after
	}
	return path.String() + rest
}

// Do answers a zero R for an empty answer, which only an R of type any expects.
func (a Api) Do[R any](ctx context.Context, call Call, body any, options ...http.RequestOption) (R, error) {
	var result R
	options = append(options, http.WithAccept(a.MediaType))
	if call.Query != nil {
		options = append(options, http.WithUrlQuery(call.Query))
	}
	if call.Method == http.MethodPut || call.Method == http.MethodDelete {
		options = append(options, http.Retryable())
	}
	if body != nil {
		payload, err := a.payload(body, call.SendsMeshObject)
		if err != nil {
			return result, err
		}
		options = append(options, http.WithJsonPayload(payload, a.MediaType))
	}
	answer, err := a.DoRequest[[]byte](ctx, call.Method, a.EndpointUrl.JoinPath(call.Path), options...)
	if err != nil {
		return result, err
	}
	if len(answer) == 0 {
		if t := reflect.TypeFor[R](); t.Kind() == reflect.Interface && t.NumMethod() == 0 {
			return result, nil
		}
		return result, fmt.Errorf("unexpected empty response body from %s %s", call.Method, call.Path)
	}
	return result, a.decode(answer, call.AnswersMeshObject, &result)
}

type page struct {
	Embedded map[string][]jsontext.Value `json:"_embedded"`
	Page     struct {
		TotalElements int `json:"totalElements"`
		TotalPages    int `json:"totalPages"`
		Number        int `json:"number"`
	} `json:"page"`
}

// List pages through the objects that call lists.
func (a Api) List[T any](ctx context.Context, call Call) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var noItem T
		for pageNumber := 0; ; pageNumber++ {
			response, err := a.Do[page](ctx, Call{Method: call.Method, Path: call.Path, Query: call.Query}, nil,
				http.WithUrlQuery(map[string]any{"page": pageNumber}))
			if err != nil && pageNumber > 0 {
				err = fmt.Errorf("cannot read page %d of the list: %w", pageNumber, err)
			}
			if err != nil {
				yield(noItem, err)
				return
			}
			items, ok := response.Embedded[a.EmbeddedRel]
			if !ok && response.Page.TotalElements > 0 {
				yield(noItem, fmt.Errorf("embedded key %s not found in paginated response", a.EmbeddedRel))
				return
			}
			for _, raw := range items {
				var item T
				if err := a.decode(raw, call.AnswersMeshObject, &item); err != nil {
					yield(noItem, err)
					return
				}
				if !yield(item, nil) {
					return
				}
			}
			if response.Page.Number >= response.Page.TotalPages-1 {
				return
			}
		}
	}
}

func (a Api) payload(body any, meshObject bool) (jsontext.Value, error) {
	encoded, err := internaljson.Marshal(body)
	if err != nil || !meshObject {
		return encoded, err
	}
	version, err := openapi.ParseApiVersion(a.ApiVersion)
	if err != nil {
		return nil, err
	}
	return openapi.MediaType{Name: a.MediaType, Kind: a.Kind, ApiVersion: version}.WithKindAndApiVersion(encoded)
}

func (a Api) decode(answer []byte, meshObject bool, target any) error {
	if meshObject {
		var header struct {
			Kind       string `json:"kind"`
			ApiVersion string `json:"apiVersion"`
		}
		if err := json.Unmarshal(answer, &header); err != nil {
			return fmt.Errorf("parsing response body as JSON failed: %w", err)
		}
		if header.Kind != a.Kind || header.ApiVersion != a.ApiVersion {
			return errors.New("meshStack answered " + header.Kind + " " + header.ApiVersion + " for " + a.Kind + " " + a.ApiVersion)
		}
	}
	if err := json.Unmarshal(answer, target); err != nil {
		return fmt.Errorf("parsing response body as JSON failed: %w", err)
	}
	return nil
}
