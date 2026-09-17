package credential

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ Credential = &ApiKey{}

type ApiKey struct {
	Endpoint     xurl.URL
	ClientId     uuid.UUID `json:"client_id"`
	ClientSecret string    `json:"client_secret"`

	// Cache is separately stored for proper concurrent access
	Cache *struct {
		Token jwt.JWT `json:"token,omitzero"`
	} `json:"-"`
}

func (apiKey *ApiKey) Identity() Identity {
	return identityOf(apiKey)
}

func (apiKey *ApiKey) CachedToken(_ context.Context, _ meshstack.Workspace) (token jwt.JWT, found bool) {
	if apiKey.Cache == nil {
		return
	}
	return apiKey.Cache.Token, true
}

func (apiKey *ApiKey) RefreshCachedToken(ctx context.Context, client http.Client, _ meshstack.Workspace) error {
	loginEndpoint := apiKey.Endpoint.JoinPath("api", "login")

	payload := struct {
		ClientId     uuid.UUID `json:"clientId"`
		ClientSecret string    `json:"clientSecret"`
	}{apiKey.ClientId, apiKey.ClientSecret}

	answer, err := client.DoRequest[struct {
		AccessToken jwt.JWT `json:"access_token"`
	}](ctx, http.MethodPost, loginEndpoint,
		http.Retryable(),
		http.WithJsonPayload(payload, "application/json"),
	)

	if httpError, ok := errors.AsType[http.Error](err); ok && httpError.IsUnauthorized() {
		return fmt.Errorf("api key %s at %s refused: %w; check the secret, or issue a new key in meshPanel", apiKey.ClientId, loginEndpoint, err)
	} else if err != nil {
		return fmt.Errorf("cannot login with api key %s at %s: %w", apiKey.ClientId, loginEndpoint, err)
	}
	if apiKey.Cache == nil {
		newCache(apiKey)
	}
	apiKey.Cache.Token = answer.AccessToken
	return nil
}
