package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
)

// Authorization produces the bearer token for each request. An implementation renews the token
// it holds once that token is close to expiry, so that [AuthorizedClient.DoRequest] only has to
// handle the 401 a token lost earlier than expected.
type Authorization interface {
	GetBearerToken(ctx context.Context) (BearerToken, error)

	// RefreshBearerToken returns the token that replaces the rejected one. Returning the
	// rejected token unchanged tells the caller that there is nothing new to try.
	RefreshBearerToken(ctx context.Context, rejected BearerToken) (BearerToken, error)

	// Scope names what meshStack grants access by, so that a 403 says it. Empty names nothing.
	Scope() string
}

func (c Client) WithAuthorization(auth Authorization) AuthorizedClient {
	return AuthorizedClient{c, auth}
}

type AuthorizedClient struct {
	Client

	Authorization Authorization
}

func (c AuthorizedClient) DoRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (R, error) {
	result, err := withBearerToken(ctx, c.Authorization, method, url, func(token RequestOption) (R, error) {
		return c.Client.DoRequest[R](ctx, method, url, append(options, token)...)
	})
	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsForbidden() && c.Authorization.Scope() != "" {
		err = fmt.Errorf("auth scope %s: %w", c.Authorization.Scope(), err)
	}
	return result, err
}

func withBearerToken[R any](ctx context.Context, auth Authorization, method string, url *url.URL, send func(token RequestOption) (R, error)) (result R, err error) {
	token, tokenErr := auth.GetBearerToken(ctx)
	if tokenErr != nil {
		return result, tokenErr
	}
	result, err = send(token.asRequestOption())

	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsUnauthorized() {
		// Clock skew between this machine and the server can make a token look valid here and
		// expired there, so one 401 earns one retry with a freshly minted token.
		refreshedToken, refreshErr := auth.RefreshBearerToken(ctx, token)
		switch {
		case refreshErr != nil:
			return result, errors.Join(err, fmt.Errorf("cannot renew the rejected token: %w", refreshErr))
		case refreshedToken == token:
			return result, err
		}
		slog.DebugContext(ctx, "retrying after 401 with a freshly minted token", "url", url.String(), "method", method)
		return send(refreshedToken.asRequestOption())
	}
	return result, err
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

func (token BearerToken) Scope() string {
	return ""
}

func (token BearerToken) asRequestOption() RequestOption {
	return withHeader("Authorization", fmt.Sprintf("Bearer %s", token))
}
