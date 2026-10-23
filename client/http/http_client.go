package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/url"
	"reflect"
	"slices"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

// Sized to ride out a full meshStack backend restart, which can leave the gateway returning 503
// for two to three minutes. This backoff sequence sums to about four minutes: 1+2+4+8+16+30*7
// seconds.
var defaultRetry = retryOptions{
	policy:     defaultRetryPolicy,
	maxRetries: 12,
	backoff:    exponentialBackoff{MinWait: 1 * time.Second, MaxWait: 30 * time.Second},
}

var sharedClient = newHttpClient(clientOptions{retry: defaultRetry})

type (
	ClientOption func(opts *clientOptions)

	clientOptions struct {
		retry       retryOptions
		noRedirects bool
	}
)

// WithoutRedirects returns a 3xx as it came.
func WithoutRedirects() ClientOption {
	return func(opts *clientOptions) {
		opts.noRedirects = true
	}
}

// WithRetry replaces the default policy, which retries a GET or a [Retryable] request on
// [RetryOnTransient] and nothing else.
func WithRetry(policy RetryPolicy) ClientOption {
	return func(opts *clientOptions) {
		opts.retry.policy = policy
	}
}

func newHttpClient(opts clientOptions) *gohttp.Client {
	client := &gohttp.Client{Transport: opts.retry.wrap(newTransport(1 * time.Minute))}
	if opts.noRedirects {
		client.CheckRedirect = func(*gohttp.Request, []*gohttp.Request) error { return gohttp.ErrUseLastResponse }
	}
	return client
}

func newTransport(silenceTimeout time.Duration) gohttp.RoundTripper {
	transport := gohttp.DefaultTransport.(*gohttp.Transport).Clone() //nolint:forcetypeassert // net/http declares it a *Transport
	transport.ResponseHeaderTimeout = silenceTimeout
	return stallGuard{Next: transport, Timeout: silenceTimeout}
}

type Client struct {
	*gohttp.Client

	UserAgent UserAgent
}

// NewClient panics on a UserAgent that does not validate, as a request has to name its front end.
// Without options, every client shares one connection pool.
func NewClient(userAgent UserAgent, options ...ClientOption) Client {
	if err := userAgent.Validate(); err != nil {
		panic(err)
	}
	if len(options) == 0 {
		return Client{sharedClient, userAgent}
	}
	opts := clientOptions{retry: defaultRetry}
	for _, option := range options {
		option(&opts)
	}
	return Client{newHttpClient(opts), userAgent}
}

// DoRequest sends one request and parses the answer as JSON, or returns it as it came, empty
// included, for an R of []byte. A non-2xx status is an Error that carries the response body,
// because an OIDC endpoint answers a refusal with an error document and that document is the only
// thing saying which refusal it was.
func (c Client) DoRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (result R, err error) {
	var body []byte
	body, err = c.doRequest(ctx, method, url, options)
	if err != nil {
		return
	}
	if raw, ok := any(&result).(*[]byte); ok {
		*raw = body
		return
	}
	if len(body) == 0 {
		// Only a call typed DoRequest[any], such as a delete, expects no content. Any other call
		// fails here, because its caller would dereference a nil result or read it as "not found".
		if t := reflect.TypeFor[R](); t.Kind() == reflect.Interface && t.NumMethod() == 0 {
			return
		}
		err = fmt.Errorf("unexpected empty response body from %s %s", method, url)
		return
	}
	if err = json.Unmarshal(body, &result); err != nil {
		err = fmt.Errorf("parsing response body as JSON failed: %w", err)
	}
	return
}

// Send returns the response whatever its status, and the caller closes its body.
func (c Client) Send(ctx context.Context, method string, url *url.URL, options ...RequestOption) (*gohttp.Response, error) {
	options = slices.Insert(options, 0, WithHeader("User-Agent", c.UserAgent.String()))
	opts := requestOptions{}
	for _, option := range options {
		option(&opts)
	}
	req, err := c.buildRequest(ctx, method, url, opts)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c Client) doRequest(ctx context.Context, method string, url *url.URL, options []RequestOption) ([]byte, error) {
	res, err := c.Send(ctx, method, url, options...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = res.Body.Close()
	}()
	return c.readBodyAndCheckSuccess(ctx, res)
}

func isSuccess(statusCode int) bool {
	return statusCode >= 200 && statusCode <= 299
}

func (c Client) readBodyAndCheckSuccess(ctx context.Context, res *gohttp.Response) ([]byte, error) {
	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read response body, status code %d: %w", res.StatusCode, err)
	}
	slog.DebugContext(ctx, "response", "status", res.StatusCode, "body", loggedBody{bytes.NewBuffer(responseBody)})

	if isSuccess(res.StatusCode) {
		return responseBody, nil
	}

	return responseBody, Error{
		Method:       res.Request.Method,
		Url:          res.Request.URL.Redacted(),
		StatusCode:   res.StatusCode,
		ResponseBody: responseBody,
	}
}

func (c Client) buildRequest(ctx context.Context, method string, url *url.URL, opts requestOptions) (*gohttp.Request, error) {
	var requestBody io.ReadWriter
	if opts.requestPayload != nil {
		requestBodyData, err := opts.requestPayload()
		if err != nil {
			return nil, fmt.Errorf("cannot build request body data: %w", err)
		}
		requestBody = bytes.NewBuffer(requestBodyData)
	}

	if opts.retryable {
		ctx = context.WithValue(ctx, retryableKey{}, true)
	}

	req, err := gohttp.NewRequestWithContext(ctx, method, url.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for _, modifier := range opts.requestModifiers {
		if err := modifier(req); err != nil {
			return nil, err
		}
	}
	slog.DebugContext(ctx, "request", "url", req.URL.String(), "method", req.Method, "headers", loggedHeaders(req.Header), "body", loggedBody{requestBody})
	return req, nil
}
