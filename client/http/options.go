package http

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	gohttp "net/http"
	"net/url"
	"reflect"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

type (
	RequestOption func(opts *requestOptions)

	requestOptions struct {
		retryable        bool
		requestPayload   func() ([]byte, error)
		requestModifiers []requestModifier
	}
	requestModifier func(req *gohttp.Request) error
)

// Retryable declares that replaying this request cannot do harm, which the default [RetryPolicy]
// requires of any method other than GET. meshStack's /api/login is the case it exists for: it mints a
// token and invalidates nothing, so a replay after a gateway 503 costs one token.
//
// Never put it on an OIDC refresh grant: that grant rotates the refresh token, and keycloak ends
// the whole session when a rotated token is used twice.
func Retryable() RequestOption {
	return func(opts *requestOptions) {
		opts.retryable = true
	}
}

// retryableKey marks a request its caller declared safe to replay. It travels in the request
// context rather than in a list on the client, because one client serves every caller and
// because gohttp.Client passes the context on to the request it issues for a redirect.
type retryableKey struct{}

func isRetryable(req *gohttp.Request) bool {
	retryable, _ := req.Context().Value(retryableKey{}).(bool)
	return retryable
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
	data, err := json.Marshal(structOrMap)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal type %T: %w", structOrMap, err)
	}
	var converted map[string]jsontext.Value
	if err := json.Unmarshal(data, &converted); err != nil {
		return nil, fmt.Errorf("cannot decode type %T into a flat map: %w", structOrMap, err)
	}
	skipZero := reflect.ValueOf(structOrMap).Kind() == reflect.Struct
	result := url.Values{}
	for key, value := range converted {
		values := []jsontext.Value{value}
		if value.Kind() == '[' {
			if err := json.Unmarshal(value, &values); err != nil {
				return nil, fmt.Errorf("cannot read %s of type %T as an array: %w", key, structOrMap, err)
			}
		}
		for _, value := range values {
			if value.Kind() == 'n' {
				continue
			}
			// A number goes in as the literal it marshalled to, never through a Go value: decoded into
			// float64 and printed again, a millisecond timestamp would arrive as 1.2345678901234568e+18.
			param := value.String()
			if value.Kind() == '"' {
				if err := json.Unmarshal(value, &param); err != nil {
					return nil, fmt.Errorf("cannot read %s of type %T as a string: %w", key, structOrMap, err)
				}
			}
			if skipZero && (param == "" || value.Kind() == 'f') {
				continue
			}
			result[key] = append(result[key], param)
		}
	}
	return result, nil
}

func appendRequestModifier(modifier requestModifier) RequestOption {
	return func(opts *requestOptions) {
		opts.requestModifiers = append(opts.requestModifiers, modifier)
	}
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

func WithBody(contentType string, body []byte) RequestOption {
	return func(opts *requestOptions) {
		if body == nil {
			return
		}
		WithHeader("Content-Type", contentType)(opts)
		opts.requestPayload = func() ([]byte, error) {
			return body, nil
		}
	}
}

// WithJsonPayload sends a value as a JSON body, and both sends and asks for the given content
// type. The type is a parameter because meshStack names a meshObject's kind and version in it.
func WithJsonPayload(payload any, contentType string) RequestOption {
	return func(opts *requestOptions) {
		if payload == nil {
			return
		}
		WithHeader("Accept", contentType)(opts)
		WithHeader("Content-Type", contentType)(opts)
		opts.requestPayload = func() ([]byte, error) {
			return json.Marshal(payload)
		}
	}
}

// WithFormPayload sends the values as an url-encoded form body — converted from a struct or a
// map as [WithUrlQuery] describes — and asks for JSON in return, which is what an OIDC grant
// expects.
func WithFormPayload(payload any) RequestOption {
	return func(opts *requestOptions) {
		if payload == nil {
			return
		}
		WithHeader("Accept", "application/json")(opts)
		WithHeader("Content-Type", "application/x-www-form-urlencoded")(opts)
		opts.requestPayload = func() ([]byte, error) {
			values, err := convertStructOrMapToUrlValues(payload)
			if err != nil {
				return nil, fmt.Errorf("cannot convert form payload: %w", err)
			}
			return []byte(values.Encode()), nil
		}
	}
}
