package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// ErrRefreshRejected reports that the identity provider refused the refresh grant: the
// keycloak session ended, was revoked, or the refresh token was reused too often. pkg/auth
// matches on it with errors.Is to say `meshstack login` rather than printing invalid_grant.
var ErrRefreshRejected = errors.New("the identity provider rejected the refresh grant")

// ErrRefreshTokenReused reports keycloak's "refresh token already used" case specifically.
// pkg/auth drops its lock, re-reads the store once and retries with whatever token it finds
// there, because another process very likely rotated it a moment ago.
var ErrRefreshTokenReused = errors.New("the refresh token was already used")

// tokenResponse is the token endpoint's answer, success and failure in one shape: keycloak
// reports a refused grant as HTTP 400 with error and error_description.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	// RefreshExpiresIn is 0 for this client's offline tokens and is deliberately not read:
	// nothing in the token says when the login dies, so nothing derives a deadline from it.
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// protocolError is an OAuth error response. It stays a distinct type so that Refresh can map
// invalid_grant to ErrRefreshRejected while Exchange, where the same code means a stale
// authorization code, reports what actually happened.
type protocolError struct {
	Status      int
	Code        string
	Description string
}

func (e *protocolError) Error() string {
	detail := e.Code
	if e.Description != "" {
		detail += ": " + e.Description
	}
	if detail == "" {
		detail = "no error code in the response body"
	}
	// Status is zero for the one case where the answer was a success: a provider that reports a
	// refusal in a 2xx body, where naming the status would say nothing.
	if e.Status == 0 {
		return fmt.Sprintf("the identity provider answered %s", detail)
	}
	return fmt.Sprintf("the identity provider answered HTTP %d, %s", e.Status, detail)
}

// Refresh runs the refresh grant. An empty name asks for scope=openid, a workspace for
// scope=openid c:<workspace>. It returns the rotated refresh token alongside the access
// token, because keycloak rotates on every refresh and losing the new one ends the session.
func Refresh(ctx context.Context, cfg ClientConfig, refreshToken string, ws workspace.Name) (newRefreshToken, accessToken string, expiresIn time.Duration, err error) {
	if refreshToken == "" {
		return "", "", 0, fmt.Errorf("cannot run the refresh grant without a refresh token")
	}

	tok, err := postToken(ctx, cfg, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {cfg.ClientId},
		// Always sent, including for the unscoped case: MC_CUSTOMER.js returns early when the
		// scope parameter is missing, so a refresh without it comes back without the claim and
		// the workspace is lost.
		"scope": {ws.WireScope()},
	})
	if err != nil {
		return "", "", 0, refreshFailure(err)
	}

	// keycloak rotates on every refresh, but a provider that answers without a new token means
	// "keep the one you have" rather than "your session is gone".
	rotated := tok.RefreshToken
	if rotated == "" {
		rotated = refreshToken
	}
	return rotated, tok.AccessToken, time.Duration(tok.ExpiresIn) * time.Second, nil
}

// refreshFailure turns the provider's own words into the sentinels pkg/auth acts on, keeping
// error_description in the message because it is the only thing that says which case it was.
func refreshFailure(err error) error {
	var perr *protocolError
	if !errors.As(err, &perr) || perr.Code != "invalid_grant" {
		return fmt.Errorf("the refresh grant failed: %w", err)
	}
	// Measured: reuse beyond refreshTokenMaxReuse says "Maximum allowed refresh token reuse
	// exceeded", while the dead session that follows says "Session doesn't have required
	// client". Only the first one is worth a retry.
	if strings.Contains(strings.ToLower(perr.Description), "reuse") {
		return fmt.Errorf("%w (%w): %w", ErrRefreshRejected, ErrRefreshTokenReused, perr)
	}
	return fmt.Errorf("%w: %w", ErrRefreshRejected, perr)
}

// Exchange completes the authorization code flow. pkg/oidc/browser calls it once it has a code.
func Exchange(ctx context.Context, cfg ClientConfig, code, redirectURI, verifier string) (refreshToken, accessToken string, expiresIn time.Duration, err error) {
	tok, err := postToken(ctx, cfg, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.ClientId},
		"code_verifier": {verifier},
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("the authorization code exchange failed: %w", err)
	}
	// Without a refresh token the login would last five minutes, so say why rather than
	// storing a credential that dies before the next command.
	if tok.RefreshToken == "" {
		return "", "", 0, fmt.Errorf(
			"the identity provider returned no refresh token, so the offline_access scope was not granted (it granted %q)", tok.Scope)
	}
	return tok.RefreshToken, tok.AccessToken, time.Duration(tok.ExpiresIn) * time.Second, nil
}

// EndSession revokes the login at the identity provider. `meshstack auth logout --revoke` calls it.
func EndSession(ctx context.Context, cfg ClientConfig, refreshToken string) error {
	if refreshToken == "" {
		return fmt.Errorf("cannot end the session without a refresh token")
	}

	// The end session endpoint takes the refresh token as its back-channel hint, which is the
	// one form that needs no browser. Revocation is the fallback for a provider that offers no
	// such endpoint; it kills the token, and with revokeRefreshToken the session with it.
	target, form := cfg.EndSessionEndpoint, url.Values{
		"client_id":     {cfg.ClientId},
		"refresh_token": {refreshToken},
	}
	if target == "" {
		target, form = cfg.RevocationEndpoint, url.Values{
			"client_id":       {cfg.ClientId},
			"token":           {refreshToken},
			"token_type_hint": {"refresh_token"},
		}
	}
	if target == "" {
		return fmt.Errorf("the identity provider advertises neither an end_session_endpoint nor a revocation_endpoint, so the session can only be ended in a browser")
	}

	// The answer carries nothing worth reading: the endpoint reports success as 204, and a
	// refusal as a status with a body that only this message ever shows.
	_, err := postForm[any](ctx, target, form)
	var httpErr http.Error
	switch {
	case errors.As(err, &httpErr):
		return fmt.Errorf("cannot end the session at %s: HTTP %d: %s", target, httpErr.StatusCode, excerpt(httpErr.ResponseBody))
	case err != nil:
		return fmt.Errorf("cannot end the session at %s: %w", target, err)
	}
	return nil
}

// postToken runs one grant. The client is public and holds no secret, so no request here
// carries client authentication.
func postToken(ctx context.Context, cfg ClientConfig, form url.Values) (tokenResponse, error) {
	if cfg.TokenEndpoint == "" || cfg.ClientId == "" {
		return tokenResponse{}, fmt.Errorf("the identity provider is not discovered yet: the token endpoint or the client id is missing")
	}

	tok, err := postForm[tokenResponse](ctx, cfg.TokenEndpoint, form)
	var httpErr http.Error
	switch {
	case errors.As(err, &httpErr):
		// A refusal is data here rather than a failure: keycloak answers HTTP 400 with the OAuth
		// error document, and that document is the only thing that distinguishes a reused refresh
		// token from a dead session. It arrives as the error's body, so it takes no second request.
		var refused tokenResponse
		if err := json.Unmarshal(httpErr.ResponseBody, &refused); err != nil {
			return tokenResponse{}, fmt.Errorf("cannot parse the answer of %s (HTTP %d) as JSON: %s",
				cfg.TokenEndpoint, httpErr.StatusCode, excerpt(httpErr.ResponseBody))
		}
		return tokenResponse{}, &protocolError{Status: httpErr.StatusCode, Code: refused.Error, Description: refused.Description}
	case err != nil:
		return tokenResponse{}, fmt.Errorf("the grant at %s failed: %w", cfg.TokenEndpoint, err)
	case tok.Error != "":
		// RFC 6749 puts a refusal in a 400 and keycloak obeys, so this covers a provider that
		// reports one in a 2xx body instead.
		return tokenResponse{}, &protocolError{Code: tok.Error, Description: tok.Description}
	case tok.AccessToken == "":
		return tokenResponse{}, fmt.Errorf("%s answered successfully but without an access token", cfg.TokenEndpoint)
	}
	return tok, nil
}

// postForm runs one grant and parses what the provider answered. R is tokenResponse for a grant
// and any where the answer carries nothing, as at the end session endpoint.
//
// No request here is marked Retryable, which is deliberate and the reason marking is opt-in: a
// refresh grant rotates the refresh token, and keycloak ends the whole session when a rotated
// token is used twice. Replaying one after a gateway hiccup would log the user out.
func postForm[R any](ctx context.Context, target string, form url.Values) (R, error) {
	var result R
	parsed, err := url.Parse(target)
	if err != nil {
		return result, fmt.Errorf("cannot build a request for %s: %w", target, err)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	return http.DoRequest[R](ctx, client(parsed), http.MethodPost, parsed, http.WithFormPayload(form))
}
