package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/url"
	"slices"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

type Client struct {
	userAgent UserAgent
}

var goClient = newGoClient()

func newGoClient() *gohttp.Client {
	transport := gohttp.DefaultTransport.(*gohttp.Transport).Clone() //nolint:forcetypeassert // net/http declares it a *Transport
	transport.ResponseHeaderTimeout = stallTimeout
	return &gohttp.Client{
		Transport: retryRoundTripper{transport},
		CheckRedirect: func(req *gohttp.Request, _ []*gohttp.Request) error {
			if requestOptionsFrom(req).noRedirects {
				return gohttp.ErrUseLastResponse
			}
			return nil
		},
	}
}

func NewClient(userAgent UserAgent) Client {
	if err := userAgent.validate(); err != nil {
		panic(err)
	}
	return Client{userAgent}
}

// DoRequest executes a HTTP request, returning the response body.
// Use WithCapturedResponseHeader as RequestOption to obtain the response headers if needed.
func (c Client) DoRequest(ctx context.Context, method string, url *url.URL, options ...RequestOption) (io.ReadCloser, error) {
	opts := newRequestOptions(slices.Insert(options, 0, WithHeader("User-Agent", c.userAgent.String())))
	ctx, cancel := context.WithCancelCause(ctx)
	req, err := opts.newRequest(ctx, method, url)
	if err != nil {
		cancel(nil)
		return nil, err
	}
	res, err := goClient.Do(req) //nolint:bodyclose // the returned body closes it, or the error path below
	if err != nil {
		cancel(nil)
		return nil, err
	}
	slog.DebugContext(ctx, "response", "status", res.StatusCode, "headers", loggedHeaders(res.Header))
	if opts.responseHeader != nil {
		*opts.responseHeader = res.Header
	}
	body := newLoggedBody(ctx, "response body", newStallGuardedBody(res.Body, cancel), opts.redactedJsonPointers)
	if statusCode := res.StatusCode; statusCode >= 200 && statusCode <= 299 {
		return body, nil
	}
	responseBody, err := ReadAllBody(body, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot read response body, status code %d: %w", res.StatusCode, err)
	}
	return nil, Error{
		Method:       res.Request.Method,
		Url:          res.Request.URL.Redacted(),
		StatusCode:   res.StatusCode,
		ResponseBody: responseBody,
	}
}

func (c Client) String() string {
	return "unauthorized client " + c.userAgent.String()
}

// UnmarshalJsonBody unmarshal the JSON body to T and closes the body.
// Matches return signature of Client.DoRequest for convenience.
// Falls back to IgnoreResponse if T is struct{}.
func UnmarshalJsonBody[T any](body io.ReadCloser, err error, unmarshalers ...*json.Unmarshalers) (result T, _ error) {
	if _, ignored := any(result).(struct{}); ignored {
		return result, IgnoreResponse(body, err)
	}
	if err != nil {
		return result, err
	}
	err = json.UnmarshalRead(body, &result, json.WithUnmarshalers(unmarshalers...))
	if err != nil {
		err = fmt.Errorf("parsing response body as JSON failed: %w", err)
	}
	return result, errors.Join(err, body.Close())
}

// ReadAllBody returns the full body as data and closes the body.
func ReadAllBody(body io.ReadCloser, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(body)
	return data, errors.Join(err, body.Close())
}

// IgnoreResponse reads the body and discards it, then closes it to ensure connection can be reused.
func IgnoreResponse(body io.ReadCloser, err error) error {
	if err != nil {
		return err
	}
	if _, err = io.CopyN(io.Discard, body, maxDrainBytes); errors.Is(err, io.EOF) {
		err = nil
	}
	return errors.Join(err, body.Close())
}

// newRequest opens the body after every modifier, so that a modifier's error leaves none open.
func (opts requestOptions) newRequest(ctx context.Context, method string, url *url.URL) (*gohttp.Request, error) {
	req, err := gohttp.NewRequestWithContext(opts.requestContext(ctx), method, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for _, modifier := range opts.requestModifiers {
		if err := modifier(req); err != nil {
			return nil, err
		}
	}
	if opts.setBody != nil {
		if err := opts.setBody(req); err != nil {
			return nil, fmt.Errorf("cannot build request body: %w", err)
		}
	}
	req.Body = newLoggedBody(ctx, "request body", req.Body, opts.redactedJsonPointers)
	slog.DebugContext(ctx, "request", "url", req.URL.String(), "method", req.Method, "headers", loggedHeaders(req.Header))
	return req, nil
}
