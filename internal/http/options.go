package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/url"
	"reflect"
)

type (
	// RequestOption is a functional option for configuring HTTP requests.
	RequestOption func(opts *requestOptions)

	requestOptions struct {
		retryable        bool
		requestPayload   func() ([]byte, error)
		requestModifiers []requestModifier
	}
	requestModifier func(req *gohttp.Request) error
)

// Retryable declares that replaying this request cannot do harm, which is the only way a method
// other than GET is ever retried.
//
// meshStack's /api/login is the case it exists for: it mints a token from an id and a secret and
// invalidates nothing, so a replay after a gateway 503 costs one token. Do not put it on a POST
// that creates something, and never on an OIDC refresh grant — that one rotates the refresh
// token, and keycloak ends the whole session when a rotated token is used twice.
func Retryable() RequestOption {
	return func(opts *requestOptions) {
		opts.retryable = true
	}
}

// retryableKey marks a request its caller declared safe to replay. It travels in the request
// context rather than in a list the client holds, because gohttp.Client passes the context on to
// the request it issues for a redirect, and because one client now serves every caller.
type retryableKey struct{}

func isRetryable(ctx context.Context) bool {
	retryable, _ := ctx.Value(retryableKey{}).(bool)
	return retryable
}

// WithUrlQuery adds URL query parameters from a query value.
//
// The given value is JSON-marshalled and decoded into a flat map, so each field becomes a query param
// named by its `json` tag. A struct passed by value is the common case: its zero-value fields are
// dropped (an implicit `omitempty`), so an unset filter needs neither a pointer nor an `omitempty`
// tag and a zero-value struct adds no params at all. A map[string]string / map[string]any is taken
// verbatim — every entry is sent, including deliberate zero values such as page=0.
//
// Values are stringified with fmt.Sprintf("%v", ...); nested objects or arrays are not supported.
func WithUrlQuery(query any) RequestOption {
	return appendRequestModifier(func(req *gohttp.Request) error {
		urlValues, err := convertStructOrMapToUrlValues(query)
		if err != nil {
			return fmt.Errorf("cannot convert url query: %w", err)
		}
		req.URL.RawQuery = urlValues.Encode()
		return nil
	})
}

func convertStructOrMapToUrlValues(structOrMap any) (url.Values, error) {
	data, err := json.Marshal(structOrMap)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal type %T: %w", structOrMap, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	// UseNumber keeps integers (e.g. page) from becoming float64 and gaining a ".0" or exponent.
	decoder.UseNumber()
	var converted map[string]any
	if err := decoder.Decode(&converted); err != nil {
		return nil, fmt.Errorf("cannot decode type %T into a flat map: %w", structOrMap, err)
	}
	// Drop zero-value fields only for a struct (passed by value, not by pointer); a map is
	// passed through as given.
	skipZero := reflect.ValueOf(structOrMap).Kind() == reflect.Struct
	result := url.Values{}
	for key, value := range converted {
		if value == nil || (skipZero && reflect.ValueOf(value).IsZero()) {
			continue
		}
		result[key] = append(result[key], fmt.Sprintf("%v", value))
	}
	return result, nil
}

func appendRequestModifier(modifier requestModifier) RequestOption {
	return func(opts *requestOptions) {
		opts.requestModifiers = append(opts.requestModifiers, modifier)
	}
}

func WithAccept(accept string) RequestOption {
	return withHeader("Accept", accept)
}

func withHeader(key, value string) RequestOption {
	return appendRequestModifier(func(req *gohttp.Request) error {
		req.Header.Set(key, value)
		return nil
	})
}

// WithJsonPayload sends a value as a JSON body, and both sends and asks for the given content type.
// meshStack names a meshObject's kind and version in that type, which is why it is a parameter
// rather than always application/json.
func WithJsonPayload(payload any, contentType string) RequestOption {
	return func(opts *requestOptions) {
		if payload == nil {
			return
		}
		WithAccept(contentType)(opts)
		withHeader("Content-Type", contentType)(opts)
		opts.requestPayload = func() ([]byte, error) {
			return json.Marshal(payload)
		}
	}
}

// WithFormPayload sends the values as an url-encoded form body (converted from struct or map, see
// WithUrlQuery), and asks for JSON in return, which is what every OIDC grant needs.
func WithFormPayload(payload any) RequestOption {
	return func(opts *requestOptions) {
		if payload == nil {
			return
		}
		WithAccept("application/json")(opts)
		withHeader("Content-Type", "application/x-www-form-urlencoded")(opts)
		opts.requestPayload = func() ([]byte, error) {
			values, err := convertStructOrMapToUrlValues(payload)
			if err != nil {
				return nil, fmt.Errorf("cannot convert form payload: %w", err)
			}
			return []byte(values.Encode()), nil
		}
	}
}
