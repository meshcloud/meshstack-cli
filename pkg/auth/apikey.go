package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// apiLoginPath is where meshStack exchanges an API key for an access token. The answer
// carries expires_in: 300 and no refresh token, so an API key access token also lives five
// minutes and is renewed by posting the id and the secret again. That is also why an API key
// can never destroy anything the way a refresh token can: /api/login mints independently of
// any previous token.
const apiLoginPath = "/api/login"

// apiLoginRetries rides out a backend that is still starting. The client this exchange used
// to live in retries GETs for about four minutes, and losing that entirely would turn a
// restarting meshStack into an immediate failure on the first authorized request.
const apiLoginRetries = 5

func apiLogin(ctx context.Context, endpoint *url.URL, clientId, clientSecret string) (token string, lifetime time.Duration, err error) {
	target := endpoint.JoinPath(apiLoginPath)
	payload, err := json.Marshal(struct {
		ClientId     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{clientId, clientSecret})
	if err != nil {
		return "", 0, err
	}

	backoff := time.Second
	for attempt := range apiLoginRetries {
		token, lifetime, err = postApiLogin(ctx, target, payload)
		if err == nil {
			return token, lifetime, nil
		}
		var status statusError
		// Only a server-side failure is worth another attempt: a 401 means the secret is
		// wrong, and retrying it four more times only delays the message.
		if errors.As(err, &status) && status.code < http.StatusInternalServerError {
			break
		}
		if attempt == apiLoginRetries-1 {
			break
		}
		slog.Debug("meshStack login failed, retrying", "url", target.String(), "attempt", attempt+1, "error", err)
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}

	var status statusError
	if errors.As(err, &status) && status.code == http.StatusUnauthorized {
		return "", 0, diags.Wrap(err, "meshStack refused this API key",
			"%s answered 401 for key id %s. Check the secret, or issue a new key in meshPanel.", target, clientId)
	}
	return "", 0, diags.Wrap(err, "could not log in to meshStack with an API key",
		"%s with key id %s failed: %v", target, clientId, err)
}

func postApiLogin(ctx context.Context, target *url.URL, payload []byte) (string, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", 0, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", 0, statusError{code: response.StatusCode, body: string(body)}
	}

	var answer struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return "", 0, fmt.Errorf("cannot parse the login answer from %s: %w", target, err)
	}
	if answer.AccessToken == "" {
		return "", 0, fmt.Errorf("%s answered without an access token", target)
	}
	return answer.AccessToken, time.Duration(answer.ExpiresIn) * time.Second, nil
}

type statusError struct {
	code int
	body string
}

func (e statusError) Error() string {
	return fmt.Sprintf("http error %d, response '%s'", e.code, e.body)
}
