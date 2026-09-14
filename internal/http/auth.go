package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
)

// Authorization produces the bearer token for each request, renewing the token it holds
// whenever the token is close to expiry. See DoAuthorizedRequest.
type Authorization interface {
	GetBearerToken(ctx context.Context) (BearerToken, error)

	// RefreshBearerToken refreshes the rejected token if possible.
	RefreshBearerToken(ctx context.Context, rejected BearerToken) (BearerToken, error)
}

func (c Client) WithAuthorization(auth Authorization) AuthorizedClient {
	return AuthorizedClient{c, auth}
}

type AuthorizedClient struct {
	Client
	Authorization Authorization
}

func (c AuthorizedClient) DoRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (result R, err error) {
	token, tokenErr := c.Authorization.GetBearerToken(ctx)
	if tokenErr != nil {
		return result, tokenErr
	}
	result, err = c.Client.DoRequest[R](ctx, method, url, append(options, token.asRequestOption())...)

	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsUnauthorized() {
		// Retry once with an explicitly refreshed token (might be that expiry is wrongly judged due to client/server clock skew)
		refreshedToken, refreshErr := c.Authorization.RefreshBearerToken(ctx, token)
		switch {
		case refreshErr != nil:
			return result, errors.Join(err, fmt.Errorf("cannot renew the rejected token: %w", refreshErr))
		case refreshedToken == token:
			return result, err
		}
		slog.DebugContext(ctx, "retrying after 401 with a freshly minted token", "url", url.String(), "method", method)
		return c.Client.DoRequest[R](ctx, method, url, append(options, refreshedToken.asRequestOption())...)
	}
	return result, err
}

// BearerToken is used in Authorization and also implements it representing a non-refreshable token.
type BearerToken string

func (token BearerToken) asRequestOption() RequestOption {
	return withHeader("Authorization", fmt.Sprintf("Bearer %s", token))
}

func (token BearerToken) GetBearerToken(_ context.Context) (BearerToken, error) {
	return token, nil
}

func (token BearerToken) RefreshBearerToken(_ context.Context, _ BearerToken) (BearerToken, error) {
	return "", fmt.Errorf("cannot renew %T", token)
}
