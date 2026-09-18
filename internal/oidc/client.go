package oidc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

type Client struct {
	http.Client

	ClientConfig

	Id string
}

type ClientConfig struct {
	Issuer                xurl.URL `json:"issuer"`
	AuthorizationEndpoint xurl.URL `json:"authorization_endpoint"`
	TokenEndpoint         xurl.URL `json:"token_endpoint"`
	// Both are optional in the discovery document, and either one ends a session.
	EndSessionEndpoint *xurl.URL `json:"end_session_endpoint"`
	RevocationEndpoint *xurl.URL `json:"revocation_endpoint"`
}

func NewClient(ctx context.Context, httpClient http.Client, issuer xurl.URL, clientId string) (Client, error) {
	clientConfig, err := httpClient.DoRequest[ClientConfig](ctx, http.MethodGet,
		issuer.JoinPath(".well-known", "openid-configuration"))
	if err != nil {
		return Client{}, err
	}
	return Client{Client: httpClient, ClientConfig: clientConfig, Id: clientId}, nil
}

type Token struct {
	AccessToken  jwt.JWT `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	Scope        string  `json:"scope"`
}

func (c Client) Refresh(ctx context.Context, refreshToken string, scopes scope.Scopes) (resp Token, err error) {
	resp, err = c.doPost[Token](ctx, c.TokenEndpoint.URL, map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.Id,
		"scope":         scopes.String(),
	})
	if err != nil {
		return resp, err
	}
	if resp.RefreshToken == "" {
		// An identity provider that does not rotate its refresh tokens returns none here.
		// Keycloak always rotates, so this branch is not the one a meshStack login takes.
		slog.DebugContext(ctx, "Re-using previous refresh token as identity provider returned empty refresh token")
		resp.RefreshToken = refreshToken
	}
	return
}

// ExchangeAuthCode ends the authorization code flow, see AuthorizationCodeFlow.Exchange.
func (c Client) ExchangeAuthCode(ctx context.Context, code string, redirectUri xurl.URL, verifier string) (resp Token, err error) {
	resp, err = c.doPost[Token](ctx, c.TokenEndpoint.URL, map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectUri,
		"client_id":     c.Id,
		"code_verifier": verifier,
	})
	if err == nil && resp.RefreshToken == "" {
		err = fmt.Errorf("the identity provider granted no refresh token, only the scopes %q", resp.Scope)
	}
	return
}

func (c Client) EndSession(ctx context.Context, refreshToken string) error {
	endpoint, payload := c.EndSessionEndpoint, map[string]any{
		"client_id":     c.Id,
		"refresh_token": refreshToken,
	}
	if endpoint == nil {
		endpoint, payload = c.RevocationEndpoint, map[string]any{
			"client_id":       c.Id,
			"token":           refreshToken,
			"token_type_hint": "refresh_token",
		}
	}
	if endpoint == nil {
		return errors.New("the identity provider advertises neither an end_session_endpoint nor a revocation_endpoint")
	}
	_, err := c.doPost[any](ctx, endpoint.URL, payload)
	return err
}

func (c Client) doPost[R any](ctx context.Context, endpoint *url.URL, payload map[string]any) (result R, err error) {
	result, err = c.DoRequest[R](ctx, http.MethodPost, endpoint, http.WithFormPayload(payload))
	if httpErr, ok := errors.AsType[http.Error](err); ok {
		var protocolErr protocolError
		if unmarshalErr := json.Unmarshal(httpErr.ResponseBody, &protocolErr); unmarshalErr != nil {
			return result, errors.Join(httpErr, unmarshalErr)
		}
		return result, errors.Join(httpErr, protocolErr)
	}
	return
}

type protocolError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e protocolError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Description)
}
