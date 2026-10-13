package http

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/url"
)

// Authorization produces the credential for each request. An implementation renews the credential
// it holds once that is close to expiry, so that [AuthorizedClient.DoRequest] only has to handle
// the 401 a credential lost earlier than expected.
type Authorization interface {
	GetCredential(ctx context.Context) (Credential, error)

	// RefreshCredential returns the credential that replaces the rejected one. Returning the
	// rejected credential unchanged tells the caller that there is nothing new to try.
	RefreshCredential(ctx context.Context, rejected Credential) (Credential, error)

	// Scope names what meshStack grants access by, so that a 403 says it. Empty names nothing.
	Scope() string
}

// Credential is comparable, so that a refresh can tell whether it produced anything new.
type Credential interface {
	authorizationHeader() string
}

func (c Client) WithAuthorization(auth Authorization) AuthorizedClient {
	return AuthorizedClient{c, auth}
}

type AuthorizedClient struct {
	Client

	Authorization Authorization
}

func (c AuthorizedClient) DoRequest[R any](ctx context.Context, method string, url *url.URL, options ...RequestOption) (R, error) {
	result, err := withCredential(ctx, c.Authorization, method, url, func(credential RequestOption) (R, error) {
		return c.Client.DoRequest[R](ctx, method, url, append(options, credential)...)
	})
	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsForbidden() && c.Authorization.Scope() != "" {
		err = fmt.Errorf("auth scope %s: %w", c.Authorization.Scope(), err)
	}
	return result, err
}

func (c AuthorizedClient) DoStreamingRequest(ctx context.Context, method string, url *url.URL, w io.Writer, options ...RequestOption) error {
	_, err := withCredential(ctx, c.Authorization, method, url, func(credential RequestOption) (any, error) {
		return nil, c.Client.DoStreamingRequest(ctx, method, url, w, append(options, credential)...)
	})
	return err
}

// Authorize is for a request that this client does not send itself, such as one a reverse proxy
// passes on. Such a request gets no second try after a 401.
func (c AuthorizedClient) Authorize(r *gohttp.Request) error {
	credential, err := c.Authorization.GetCredential(r.Context())
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", credential.authorizationHeader())
	return nil
}

func withCredential[R any](ctx context.Context, auth Authorization, method string, url *url.URL, send func(credential RequestOption) (R, error)) (result R, err error) {
	credential, credentialErr := auth.GetCredential(ctx)
	if credentialErr != nil {
		return result, credentialErr
	}
	result, err = send(withAuthorizationHeader(credential))

	if httpErr, ok := errors.AsType[Error](err); ok && httpErr.IsUnauthorized() {
		// Clock skew between this machine and the server can make a token look valid here and
		// expired there, so one 401 earns one retry with a freshly minted token.
		refreshed, refreshErr := auth.RefreshCredential(ctx, credential)
		switch {
		case refreshErr != nil:
			return result, errors.Join(err, fmt.Errorf("cannot renew the rejected credential: %w", refreshErr))
		case refreshed == credential:
			return result, err
		}
		slog.DebugContext(ctx, "retrying after 401 with a freshly minted credential", "url", url.String(), "method", method)
		return send(withAuthorizationHeader(refreshed))
	}
	return result, err
}

func withAuthorizationHeader(credential Credential) RequestOption {
	return WithHeader("Authorization", credential.authorizationHeader())
}

type BearerToken string

func (token BearerToken) GetCredential(context.Context) (Credential, error) {
	return token, nil
}

func (token BearerToken) RefreshCredential(context.Context, Credential) (Credential, error) {
	return nil, fmt.Errorf("cannot renew %T", token)
}

func (token BearerToken) Scope() string {
	return ""
}

func (token BearerToken) authorizationHeader() string {
	return "Bearer " + string(token)
}

type BasicAuth struct {
	Username, Password string
}

func (basic BasicAuth) GetCredential(context.Context) (Credential, error) {
	return basic, nil
}

func (basic BasicAuth) RefreshCredential(context.Context, Credential) (Credential, error) {
	return nil, fmt.Errorf("cannot renew %T", basic)
}

func (basic BasicAuth) Scope() string {
	return ""
}

func (basic BasicAuth) authorizationHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(basic.Username+":"+basic.Password))
}
