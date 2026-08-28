package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

type Client struct {
	http.Client
	CliClientId string // the public CLI client from /mesh/info
	ClientConfig
}

type ClientConfig struct {
	Issuer                xurl.URL `json:"issuer"`
	AuthorizationEndpoint xurl.URL `json:"authorization_endpoint"`
	TokenEndpoint         xurl.URL `json:"token_endpoint"`
	// Both are optional in the discovery document, and one of them ends a session.
	EndSessionEndpoint *xurl.URL `json:"end_session_endpoint"`
	RevocationEndpoint *xurl.URL `json:"revocation_endpoint"`
}

// NewClient discovers a new client starting from the /mesh/info endpoint of the meshstack instance.
func NewClient(ctx context.Context, httpClient http.Client, rootUrl *url.URL) (Client, error) {
	meshInfo, err := client.NewMeshInfoClient(ctx, rootUrl, httpClient).Read(ctx)
	if err != nil {
		return Client{}, err
	}
	clientConfig, err := httpClient.DoRequest[ClientConfig](ctx, http.MethodGet, meshInfo.Issuer.JoinPath(".well-known", "openid-configuration"))
	if err != nil {
		return Client{}, err
	}
	return Client{httpClient, meshInfo.CliClientId, clientConfig}, nil
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

// Token is the token endpoint's successful answer.
type Token struct {
	AccessToken  jwt.JWT `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	Scope        string  `json:"scope"`
}

func (c Client) Refresh(ctx context.Context, refreshToken string, workspace workspace.Name) (resp Token, err error) {
	scopes := scope.Scopes{"openid"}
	if !workspace.Empty() {
		scopes = append(scopes, workspace.Scope())
	}
	resp, err = c.doPost[Token](ctx, c.TokenEndpoint.URL, map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.CliClientId,
		"scope":         scopes.String(),
	})
	if err != nil {
		return resp, err
	}
	if resp.RefreshToken == "" {
		// Keycloak does rotate refresh token on every call though (also to detect replay attacks)!
		slog.DebugContext(ctx, "Re-using previous refresh token as identity provider returned empty refresh token")
		resp.RefreshToken = refreshToken
	}
	return
}

// Exchange completes the authorization code flow. pkg/oidc/browser calls it once it has a code.
func (c Client) Exchange(ctx context.Context, code, redirectUri, verifier string) (resp Token, err error) {
	resp, err = c.doPost[Token](ctx, c.TokenEndpoint.URL, map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectUri,
		"client_id":     c.CliClientId,
		"code_verifier": verifier,
	})
	if err == nil && resp.RefreshToken == "" {
		err = fmt.Errorf("the identity provider granted no refresh token, only the scopes %q", resp.Scope)
	}
	return
}

// EndSession ends the login at the identity provider. `meshstack auth logout --revoke` calls it.
func (c Client) EndSession(ctx context.Context, refreshToken string) error {
	endpoint, payload := c.EndSessionEndpoint, map[string]any{
		"client_id":     c.CliClientId,
		"refresh_token": refreshToken,
	}
	if endpoint == nil {
		endpoint, payload = c.RevocationEndpoint, map[string]any{
			"client_id":       c.CliClientId,
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
