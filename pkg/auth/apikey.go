package auth

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// apiLoginPath is where meshStack exchanges an API key for an access token. The answer
// carries expires_in: 300 and no refresh token, so an API key access token also lives five
// minutes and is renewed by posting the id and the secret again. That is also why an API key
// can never destroy anything the way a refresh token can: /api/login mints independently of
// any previous token.
const apiLoginPath = "/api/login"

func apiLogin(ctx context.Context, endpoint *url.URL, clientId, clientSecret string) (token string, lifetime time.Duration, err error) {
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
	answer, err := http.DoRequest[struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}](ctx, http.NewClient(endpoint, userAgent, nil), "POST", target,
		http.Retryable(),
		http.WithPayload(payload, "application/json"),
	)

	var httpErr http.Error
	switch {
	case err == nil && answer.AccessToken == "":
		return "", 0, diags.Errorf("could not log in to meshStack with an API key",
			"%s answered without an access token.", target)
	case err == nil:
		return answer.AccessToken, time.Duration(answer.ExpiresIn) * time.Second, nil
	case errors.As(err, &httpErr) && httpErr.IsUnauthorized():
		return "", 0, diags.Wrap(err, "meshStack refused this API key",
			"%s answered 401 for key id %s. Check the secret, or issue a new key in meshPanel.", target, clientId)
	default:
		return "", 0, diags.Wrap(err, "could not log in to meshStack with an API key",
			"%s with key id %s failed: %v", target, clientId, err)
	}
}
