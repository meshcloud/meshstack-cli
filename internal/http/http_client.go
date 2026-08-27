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
	"strings"
	"time"
)

// requestTimeout bounds one request, retries included, on top of whatever deadline the caller's
// context carries. It exists for the case a server accepts the connection and then goes quiet,
// which no context deadline covers unless the caller set one. A caller that needs a tighter
// bound — the OIDC exchanges do — sets a context deadline instead of asking for another client.
const requestTimeout = 2 * time.Minute

// sharedClient is the one HTTP client in the process. Nothing else builds one; .golangci.yml
// holds that rule and says why.
//
// It carries the retry transport for everyone, which is safe because retrying is decided per
// request rather than per client: GET, PUT and DELETE are replayed, and every other method only
// if the caller asked with Retryable. So the OIDC refresh grant reaches this client and is never
// replayed, which matters — a refresh grant rotates the refresh token, and keycloak ends the
// whole session when one is reused.
var sharedClient = withRetry(&gohttp.Client{Timeout: requestTimeout}, retryOptions{
	// Sized to ride out a full meshStack backend restart, which can leave the gateway
	// returning 503 for two to three minutes. This backoff sequence sums to about four
	// minutes: 1+2+4+8+16+30*7 seconds.
	MaxRetries: 12,
	Backoff:    ExponentialBackoff{MinWait: 1 * time.Second, MaxWait: 30 * time.Second},
})

// NewClient addresses one API with one identity. The client underneath is shared with every
// other target, so what this adds is the root URL, the user agent and the authorization — the
// three things that differ per API, and the reason this is not one value for the whole process.
func NewClient(rootUrl *url.URL, userAgent string, auth Authorization) Client {
	return Client{sharedClient, rootUrl, userAgent, auth}
}

// Client addresses one API over the process's shared [gohttp.Client], adding the root URL,
// the user agent and the authorization, and request handling through RequestOption.
type Client struct {
	*gohttp.Client
	RootUrl       *url.URL
	UserAgent     string
	Authorization Authorization
}

func DoAuthorizedRequest[R any](ctx context.Context, c Client, method string, url *url.URL, options ...RequestOption) (result R, err error) {
	if c.Authorization == nil {
		return result, fmt.Errorf("cannot do authorized request with unconfigured authorization")
	}
	token, err := c.Authorization.BearerToken(ctx)
	if err != nil {
		return result, err
	}
	withAuthBearerToken := func(token string) []RequestOption {
		return append(options, withHeader("Authorization", "Bearer "+token))
	}
	result, err = DoRequest[R](ctx, c, method, url, withAuthBearerToken(token)...)

	// A 401 on a token the authorization believed valid forces exactly one refresh. The
	// renewal grace window covers a request issued just before expiry and modest clock skew,
	// but not a clock that is minutes wrong — which containers with a frozen clock really are.
	// One bounded retry turns that from a confusing failure into a hiccup. Re-running DoRequest
	// is safe because buildRequest encodes the payload afresh on every call.
	httpErr, isHttpErr := errors.AsType[Error](err)
	if !isHttpErr || !httpErr.IsUnauthorized() {
		return result, err
	}
	refreshed, refreshErr := c.Authorization.RefreshBearerToken(ctx, token)
	switch {
	case refreshErr != nil:
		// Both errors travel: the 401 is what the caller's request ran into, and the renewal
		// failure is what says why nothing better could be tried.
		return result, errors.Join(err, fmt.Errorf("cannot renew the rejected token: %w", refreshErr))
	case refreshed == token:
		// A refresh that produced the same token has nothing new to try, so the 401 stands.
		return result, err
	}
	slog.DebugContext(ctx, "retrying after 401 with a freshly minted token", "url", url.String(), "method", method)
	return DoRequest[R](ctx, c, method, url, withAuthBearerToken(refreshed)...)
}

// DoRequest sends one request and parses the answer as JSON. A non-2xx status is an Error
// carrying that status and the response body, so a caller that reads an error document — the OIDC
// endpoints answer one, and it is the only thing that says which refusal it was — takes it from
// there rather than from a second request.
func DoRequest[R any](ctx context.Context, c Client, method string, url *url.URL, options ...RequestOption) (result R, err error) {
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
		// The body travels with the error. A 2xx that is not the document the caller asked for is
		// usually something other than the API answering — a proxy, a captive portal, an SSO login
		// page — and the body is the only thing that says so.
		err = fmt.Errorf("cannot parse the answer of %s %s as JSON: %w: %s", method, url, err, excerpt(body))
	}
	return
}

// excerpt makes a response body fit in one line of an error message.
func excerpt(body []byte) string {
	line := strings.Join(strings.Fields(string(body)), " ")
	if len(line) > 200 {
		return line[:200] + "..."
	}
	return line
}

func (c Client) doRequest(ctx context.Context, method string, url *url.URL, options []RequestOption) ([]byte, error) {
	options = slices.Insert(options, 0,
		withHeader("User-Agent", c.UserAgent),
	)
	opts := requestOptions{}
	for _, option := range options {
		option(&opts)
	}
	if opts.optionErr != nil {
		return nil, opts.optionErr
	}
	req, err := c.buildRequest(ctx, method, *url, opts)
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

func (c Client) buildRequest(ctx context.Context, method string, url url.URL, opts requestOptions) (*gohttp.Request, error) {
	if len(opts.extraPathElems) > 0 {
		url = *url.JoinPath(opts.extraPathElems...)
	}

	if len(opts.urlQueryParams) > 0 {
		query := url.Query()
		for k, v := range opts.urlQueryParams {
			query.Set(k, v)
		}
		url.RawQuery = query.Encode()
	}

	var requestBody io.ReadWriter
	switch {
	case opts.requestPayload != nil:
		requestBody = new(bytes.Buffer)
		if err := json.NewEncoder(requestBody).Encode(opts.requestPayload); err != nil {
			return nil, fmt.Errorf("failed to encode request body payload: %w", err)
		}
	case opts.rawPayload != nil:
		requestBody = bytes.NewBuffer(opts.rawPayload)
	}

	if opts.retryable {
		ctx = context.WithValue(ctx, retryableKey{}, true)
	}
	req, err := gohttp.NewRequestWithContext(ctx, method, url.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for _, requestModifier := range opts.requestModifiers {
		requestModifier(req)
	}
	slog.DebugContext(ctx, "request", "url", req.URL.String(), "method", req.Method, "headers", loggedHeaders(req.Header), "body", loggedBody{requestBody})
	return req, err
}
