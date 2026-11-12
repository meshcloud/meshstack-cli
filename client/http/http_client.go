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

func (c Client) String() string {
	return "unauthorized client " + c.userAgent.String()
}

// UnmarshalJsonBody closes the body.
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

func ReadAllBody(body io.ReadCloser, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(body)
	return data, errors.Join(err, body.Close())
}

func IgnoreResponse(body io.ReadCloser, err error) error {
	if err != nil {
		return err
	}
	if _, err = io.CopyN(io.Discard, body, maxDrainBytes); errors.Is(err, io.EOF) {
		err = nil
	}
	return errors.Join(err, body.Close())
}

// DoRequest leaves the body to the caller. A non-2xx status is an Error that carries the response body,
// because an OIDC endpoint answers a refusal with an error document and that document is the only
// thing saying which refusal it was.
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
	if isSuccess(res.StatusCode) {
		return body, nil
	}
	responseBody, err := io.ReadAll(body)
	_ = body.Close()
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

func isSuccess(statusCode int) bool {
	return statusCode >= 200 && statusCode <= 299
}
