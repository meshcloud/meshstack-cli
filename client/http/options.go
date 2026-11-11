package http

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"io"
	"maps"
	gohttp "net/http"
	"net/url"
	"os"
	"reflect"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

type (
	RequestOption func(opts *requestOptions)

	requestOptions struct {
		requestModifiers []requestModifier
		retry            retryOptions
		noRedirects      bool
		setBody          requestModifier
		responseHeader   *gohttp.Header
	}
	requestModifier func(req *gohttp.Request) error
)

func newRequestOptions(options []RequestOption) requestOptions {
	opts := requestOptions{
		retry: retryOptions{
			policy: func(method string) RetryOn {
				if method == MethodGet || method == MethodDelete {
					return RetryOnTransient
				}
				// The server did not act on a request it answered with one of these.
				return RetryOnBusy | RetryOnLocked
			},
			// Outlasts a meshStack backend restart, whose gateway can answer 503 for three minutes:
			// 1+2+4+8+16+30*11 seconds is about six minutes.
			maxRetries: 16,
			backoff:    exponentialBackoff{MinWait: 1 * time.Second, MaxWait: 30 * time.Second},
		},
	}
	for _, option := range options {
		option(&opts)
	}
	return opts
}

type requestOptionsKey struct{}

func (opts requestOptions) requestContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestOptionsKey{}, opts)
}

func requestOptionsFrom(req *gohttp.Request) requestOptions {
	if opts, ok := req.Context().Value(requestOptionsKey{}).(requestOptions); ok {
		return opts
	}
	return newRequestOptions(nil)
}

func appendRequestModifier(modifier requestModifier) RequestOption {
	return func(opts *requestOptions) {
		opts.requestModifiers = append(opts.requestModifiers, modifier)
	}
}

func Retryable(retryOn RetryOn) RequestOption {
	return func(opts *requestOptions) {
		opts.retry.policy = func(string) RetryOn {
			return retryOn
		}
	}
}

func WithoutRedirects() RequestOption {
	return func(opts *requestOptions) {
		opts.noRedirects = true
	}
}

func WithCapturedResponseHeader(header *gohttp.Header) RequestOption {
	return func(opts *requestOptions) {
		opts.responseHeader = header
	}
}

// WithUrlQuery sends each field of query as a query parameter named by its `json` tag. A struct
// passed by value leaves out every field that has the zero value, so an unset filter needs neither
// a pointer nor `omitempty`. A map is sent as given, so a deliberate zero such as page=0 stays.
// Each value is sent as the JSON literal it marshals to, with a string unquoted, and an array as the
// parameter repeated. Nested objects are not supported.
func WithUrlQuery(query any) RequestOption {
	return appendRequestModifier(func(req *gohttp.Request) error {
		urlValues, err := convertStructOrMapToUrlValues(query)
		if err != nil {
			return fmt.Errorf("cannot convert url query: %w", err)
		}
		// Merged rather than assigned: MeshObjectClient.List adds its own page parameter to
		// whatever filter the caller passed, and overwriting would drop a required parameter
		// such as buildingBlockDefinitionUuid, which the backend then rejects.
		merged := req.URL.Query()
		maps.Copy(merged, urlValues)
		req.URL.RawQuery = merged.Encode()
		return nil
	})
}

func convertStructOrMapToUrlValues(structOrMap any) (url.Values, error) {
	encoded, err := json.Marshal(structOrMap)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal type %T: %w", structOrMap, err)
	}
	var converted map[string]jsontext.Value
	if err := json.Unmarshal(encoded, &converted); err != nil {
		return nil, fmt.Errorf("cannot decode type %T into a flat map: %w", structOrMap, err)
	}
	skipZero := reflect.ValueOf(structOrMap).Kind() == reflect.Struct
	result := url.Values{}
	for key, value := range converted {
		values := []jsontext.Value{value}
		if value.Kind() == jsontext.KindBeginArray {
			if err := json.UnmarshalRead(bytes.NewReader(value), &values); err != nil {
				return nil, fmt.Errorf("cannot read %s of type %T as an array: %w", key, structOrMap, err)
			}
		}
		for _, value := range values {
			if value.Kind() == jsontext.KindNull {
				continue
			}
			// A number goes in as the literal it marshalled to, never through a Go value: decoded into
			// float64 and printed again, a millisecond timestamp would arrive as 1.2345678901234568e+18.
			param := value.String()
			if value.Kind() == jsontext.KindString {
				if err := json.UnmarshalRead(bytes.NewReader(value), &param); err != nil {
					return nil, fmt.Errorf("cannot read %s of type %T as a string: %w", key, structOrMap, err)
				}
			}
			if skipZero && (param == "" || value.Kind() == jsontext.KindFalse) {
				continue
			}
			result[key] = append(result[key], param)
		}
	}
	return result, nil
}

func WithHeader(key string, values ...string) RequestOption {
	return appendRequestModifier(func(req *gohttp.Request) error {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
		return nil
	})
}

func WithContentType(contentType string) RequestOption {
	return WithHeader("Content-Type", contentType)
}

func WithFileRequest(path, contentType string) RequestOption {
	return func(opts *requestOptions) {
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		WithContentType(contentType)(opts)
		opts.setBody = func(req *gohttp.Request) error {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			return setBody(req, info.Size(), func() (io.ReadCloser, error) {
				return os.Open(path) //nolint:gosec // the caller names the file it uploads
			})
		}
	}
}

// wireCompatibility keeps the body encoding/json v1 produced: the meshObject API may read null and
// an empty collection differently on a PUT, and the Terraform provider hashes this JSON.
var wireCompatibility = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

func WithJsonRequest(payload any, contentType string) RequestOption {
	return func(opts *requestOptions) {
		if contentType == "" {
			contentType = "application/json"
		}

		WithHeader("Accept", contentType)(opts)
		WithContentType(contentType)(opts)
		WithJsonBody(payload)(opts)
	}
}

func WithJsonBody(payload any) RequestOption {
	return func(opts *requestOptions) {
		if payload != nil {
			opts.setBody = setBytesBody(func() ([]byte, error) {
				return json.Marshal(payload, wireCompatibility)
			})
		}
	}
}

// WithFormRequest sends the values as an url-encoded form body — converted from a struct or a
// map as [WithUrlQuery] describes — and asks for JSON in return, which is what an OIDC grant
// expects.
func WithFormRequest(payload any) RequestOption {
	return func(opts *requestOptions) {
		if payload == nil {
			return
		}
		WithHeader("Accept", "application/json")(opts)
		WithContentType("application/x-www-form-urlencoded")(opts)
		opts.setBody = setBytesBody(func() ([]byte, error) {
			values, err := convertStructOrMapToUrlValues(payload)
			if err != nil {
				return nil, fmt.Errorf("cannot convert form payload: %w", err)
			}
			return []byte(values.Encode()), nil
		})
	}
}

func setBytesBody(build func() ([]byte, error)) requestModifier {
	return func(req *gohttp.Request) error {
		body, err := build()
		if err != nil {
			return err
		}
		return setBody(req, int64(len(body)), func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		})
	}
}

func setBody(req *gohttp.Request, length int64, open func() (io.ReadCloser, error)) (err error) {
	if length == 0 {
		// A ContentLength of 0 with a body would mean an unknown length, sent chunked.
		open = func() (io.ReadCloser, error) { return gohttp.NoBody, nil }
	}
	req.ContentLength, req.GetBody = length, open
	req.Body, err = open()
	return err
}
