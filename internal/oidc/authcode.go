package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

// AuthorizationCodeFlow is one run of the authorization code flow with PKCE.
type AuthorizationCodeFlow struct {
	Client

	RedirectURI xurl.URL
	verifier    string
	state       string
}

func (c Client) NewAuthorizationCode(redirectURI xurl.URL) AuthorizationCodeFlow {
	return AuthorizationCodeFlow{Client: c, RedirectURI: redirectURI, verifier: randomString(), state: randomString()}
}

func (a AuthorizationCodeFlow) Exchange(ctx context.Context, code string) (Token, error) {
	return a.ExchangeAuthCode(ctx, code, a.RedirectURI, a.verifier)
}

// BrowserUrl is where the person has to go with a browser.
func (a AuthorizationCodeFlow) BrowserUrl() xurl.URL {
	challenge := sha256.Sum256([]byte(a.verifier))
	// scopes never include c:<workspace>: a login is unscoped and the workspace arrives later
	scopes := scope.Scopes{scope.OpenId, scope.Profile, scope.Email, scope.OfflineAccess}
	authURL := a.AuthorizationEndpoint.Clone()
	authURL.RawQuery = url.Values{
		"response_type":         {"code"},
		"client_id":             {a.Id},
		"redirect_uri":          {a.RedirectURI.String()},
		"scope":                 {scopes.String()},
		"state":                 {a.state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}.Encode()
	return authURL
}

func (a AuthorizationCodeFlow) CheckState(state string) error {
	if subtle.ConstantTimeCompare([]byte(state), []byte(a.state)) != 1 {
		return errors.New("the login redirect carried the wrong state parameter, so it did not belong to this login")
	}
	return nil
}

// randomString is the source of both the PKCE verifier and the state parameter: 32 bytes, which
// is the top of the range RFC 7636 allows for a verifier.
func randomString() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
