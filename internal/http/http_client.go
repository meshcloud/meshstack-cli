package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/url"
	"reflect"
	"slices"
	"time"
)

// sharedClient is the only place where instance of &gohttp.Client is created, allowing for connection pooling a
// and consistent management of retry config / timeouts.
var sharedClient = func() (client *gohttp.Client) {
	client = &gohttp.Client{
		// Timeout covers the whole request (from connect to receiving response body) and is an upper limit.
		Timeout: 1 * time.Minute,
	}
	RetryOptions{
		// Sized to ride out a full meshStack backend restart, which can leave the gateway
		// returning 503 for two to three minutes. This backoff sequence sums to about four
		// minutes: 1+2+4+8+16+30*7 seconds.
		MaxRetries: 12,
		Backoff:    ExponentialBackoff{MinWait: 1 * time.Second, MaxWait: 30 * time.Second},
	}.ApplyTo(client)
	return
}()

func NewClient(userAgent string, auth Authorization) Client {
	return Client{sharedClient, userAgent, auth}
}

type Client struct {
	*gohttp.Client
	UserAgent     string
	Authorization Authorization
}

func (c Client) DoAuthorizedRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (result R, err error) {
	if c.Authorization == nil {
		return result, fmt.Errorf("cannot do authorized request with unconfigured authorization")
	}
	withAuthBearerToken := func(token string) []RequestOption {
		return append(options, withHeader("Authorization", "Bearer "+token))
	}

	cachedToken, tokenErr := c.Authorization.BearerToken(ctx)
	if tokenErr != nil {
		return result, tokenErr
	}
	result, err = c.DoRequest[R](ctx, method, url, withAuthBearerToken(cachedToken)...)

	// A 401 on a token the authorization believed valid forces exactly one refresh. The
	// renewal grace window covers a request issued just before expiry and modest clock skew,
	// but not a clock that is minutes wrong — which containers with a frozen clock really are.
	// One bounded retry turns that from a confusing failure into a hiccup. Re-running DoRequest
	// is safe because buildRequest encodes the payload afresh on every call.
	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsUnauthorized() {
		refreshedToken, refreshErr := c.Authorization.RefreshBearerToken(ctx, cachedToken)
		switch {
		case refreshErr != nil:
			return result, errors.Join(err, fmt.Errorf("cannot renew the rejected token: %w", refreshErr))
		case refreshedToken == cachedToken:
			return result, err
		}
		slog.DebugContext(ctx, "retrying after 401 with a freshly minted token", "url", url.String(), "method", method)
		return c.DoRequest[R](ctx, method, url, withAuthBearerToken(refreshedToken)...)
	}
	return result, err
}

// DoRequest sends one request and parses the answer as JSON. A non-2xx status is an Error
// carrying that status and the response body, so a caller that reads an error document — the OIDC
// endpoints answer one, and it is the only thing that says which refusal it was — takes it from
// there rather than from a second request.
func (c Client) DoRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (result R, err error) {
	var body []byte
	body, err = c.doRequest(ctx, method, url, options)
	if err != nil {
		return
	}
	if len(body) == 0 {
		// An empty body is expected only for no-content calls, which are typed DoRequest[any] (e.g.
		// trigger-run, delete) and ignore the result. For a call that expects an object (a pointer or a
		// concrete struct), an empty 2xx body is unexpected — fail loudly instead of returning a nil/zero
		// value that the caller would dereference or mistake for a 404/"not found".
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

func (c Client) doRequest(ctx context.Context, method string, url *url.URL, options []RequestOption) ([]byte, error) {
	options = slices.Insert(options, 0,
		withHeader("User-Agent", c.UserAgent),
	)
	opts := requestOptions{}
	for _, option := range options {
		option(&opts)
	}
	req, err := c.buildRequest(ctx, method, url, opts)
	if err != nil {
		return nil, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = res.Body.Close()
	}()
	return c.readBodyAndCheckSuccess(ctx, res)
}

func (c Client) readBodyAndCheckSuccess(ctx context.Context, res *gohttp.Response) ([]byte, error) {
	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read response body, status code %d: %w", res.StatusCode, err)
	}
	slog.DebugContext(ctx, "response", "status", res.StatusCode, "body", loggedBody{bytes.NewBuffer(responseBody)})

	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		return responseBody, nil
	}

	return responseBody, Error{
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
	return req, err
}
