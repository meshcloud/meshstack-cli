package auth

import (
	"context"
	"errors"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
)

// apiLoginPath is where meshStack exchanges an API key for an access token. The answer
// carries expires_in: 300 and no refresh token, so an API key access token also lives five
// minutes and is renewed by posting the id and the secret again. That is also why an API key
// can never destroy anything the way a refresh token can: /api/login mints independently of
// any previous token.
const apiLoginPath = "/api/login"

func apiLogin(ctx context.Context, endpoint xurl.URL, clientId, clientSecret string) (jwt.JWT, error) {
	target := endpoint.JoinPath(apiLoginPath)
	payload := struct {
		ClientId     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{clientId, clientSecret}

	// Retryable is what rides out a backend that is still starting, and it is safe here for the
	// same reason /api/login can destroy nothing: the exchange mints from the id and the secret
	// and invalidates no previous token, so a replay after a gateway 503 costs one token.
	//
	// A wrong secret is not retried, because the transport only replays a gateway's own answers
	// — 429, 502, 503, 504 and a connection that never came up. Which is also why a meshStack
	// behind a Kubernetes gateway is the case this covers: the gateway answers while the
	// application behind it restarts.
	answer, err := http.NewClient(userAgent, nil).DoRequest[struct {
		// The deadline comes with the token, in its exp claim, rather than from the
		// expires_in the answer also carries.
		AccessToken jwt.JWT `json:"access_token"`
	}](ctx, http.MethodPost, target,
		http.Retryable(),
		http.WithJsonPayload(payload, "application/json"),
	)

	var httpErr http.Error
	switch {
	case err == nil && answer.AccessToken.String == "":
		return jwt.JWT{}, diags.Errorf("could not log in to meshStack with an API key",
			"%s answered without an access token.", target)
	case err == nil:
		return answer.AccessToken, nil
	case errors.As(err, &httpErr) && httpErr.IsUnauthorized():
		return jwt.JWT{}, diags.Wrap(err, "meshStack refused this API key",
			"%s answered 401 for key id %s. Check the secret, or issue a new key in meshPanel.", target, clientId)
	default:
		return jwt.JWT{}, diags.Wrap(err, "could not log in to meshStack with an API key",
			"%s with key id %s failed: %v", target, clientId, err)
	}
}
