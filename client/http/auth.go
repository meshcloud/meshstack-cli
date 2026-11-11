package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/url"
	"strings"
)

// Authorization produces the bearer token for each request. An implementation renews the token
// it holds once that token is close to expiry, so that [AuthorizedClient.DoRequest] only has to
// handle the 401 a token lost earlier than expected.
type Authorization interface {
	GetBearerToken(ctx context.Context) (BearerToken, error)

	// RefreshBearerToken returns the token that replaces the rejected one. Returning the
	// rejected token unchanged tells the caller that there is nothing new to try.
	RefreshBearerToken(ctx context.Context, rejected BearerToken) (BearerToken, error)

	fmt.Stringer
}

type AuthorizedClient struct {
	client Client
	auth   Authorization
}

func (c Client) WithAuthorization(auth Authorization) AuthorizedClient {
	return AuthorizedClient{c, auth}
}

func (c AuthorizedClient) DoRequest(ctx context.Context, method string, url *url.URL, options ...RequestOption) (io.ReadCloser, error) {
	token, err := c.auth.GetBearerToken(ctx)
	if err != nil {
		return nil, err
	}
	body, err := c.client.DoRequest(ctx, method, url, append(options, token.asRequestOption())...)
	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsUnauthorized() {
		// Clock skew between this machine and the server can make a token look valid here and
		// expired there, so one 401 earns one retry with a freshly minted token.
		refreshedToken, refreshErr := c.auth.RefreshBearerToken(ctx, token)
		switch {
		case refreshErr != nil:
			err = errors.Join(err, fmt.Errorf("cannot renew the rejected token: %w", refreshErr))
		case refreshedToken != token:
			slog.DebugContext(ctx, "retrying after 401 with a freshly minted token", "url", url.String(), "method", method)
			body, err = c.client.DoRequest(ctx, method, url, append(options, refreshedToken.asRequestOption())...)
		}
	}
	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsForbidden() {
		err = fmt.Errorf("auth %s: %w", c.auth, err)
	}
	return body, err
}

func (c AuthorizedClient) String() string {
	return "authorized client " + c.client.userAgent.String()
}

func (c AuthorizedClient) ProxyTransport() gohttp.RoundTripper {
	return proxyTransport{c}
}

type proxyTransport struct {
	AuthorizedClient
}

func (t proxyTransport) RoundTrip(in *gohttp.Request) (*gohttp.Response, error) {
	out := in.Clone(in.Context())
	out.Header.Set("User-Agent", strings.TrimSpace(in.Header.Get("User-Agent")+" "+t.client.userAgent.String()))
	if in.Header.Get("Authorization") != "" {
		token, err := t.auth.GetBearerToken(in.Context())
		if err != nil {
			return nil, err
		}
		out.Header.Set("Authorization", "Bearer "+string(token))
	}
	return goClient.Transport.RoundTrip(out)
}

// BearerToken is both the token Authorization produces and an Authorization of its own, for a
// caller that holds one static token.
type BearerToken string

func (token BearerToken) GetBearerToken(_ context.Context) (BearerToken, error) {
	return token, nil
}

func (token BearerToken) RefreshBearerToken(_ context.Context, _ BearerToken) (BearerToken, error) {
	return "", fmt.Errorf("cannot renew %T", token)
}

// String shows only the start of a long token, so that formatting one never leaks it.
func (token BearerToken) String() string {
	if len(token) < 32 {
		return "BearerToken [REDACTED]"
	}
	return "BearerToken " + string(token[:6]) + "...[REDACTED]"
}

func (token BearerToken) asRequestOption() RequestOption {
	return WithHeader("Authorization", "Bearer "+string(token))
}
