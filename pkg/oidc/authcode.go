package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// AuthorizationCodeFlow is one run of the authorization code flow with PKCE. It holds the two
// values that have to survive from the authorization request to the token request and that must
// not leave this package: the PKCE verifier and the state parameter. An interactive front end is
// left with a loopback listener, a way to open a browser, and a page to show when the redirect
// lands.
type AuthorizationCodeFlow struct {
	client      Client
	redirectURI xurl.URL
	verifier    string
	state       string
}

// NewAuthorizationCode begins a flow that redirects to redirectURI. The caller binds its listener
// first, because the port is part of the URI and the token request has to echo the URI back
// unchanged.
func (c Client) NewAuthorizationCode(redirectURI xurl.URL) AuthorizationCodeFlow {
	return AuthorizationCodeFlow{client: c, redirectURI: redirectURI, verifier: randomString(), state: randomString()}
}

// URL is where the person has to go.
func (a AuthorizationCodeFlow) URL() *url.URL {
	// The challenge is the hash itself, so it is bytes rather than text, and RFC 7636 says to
	// write those bytes as base64url without padding. url.Values.Encode() percent-escapes the
	// string it is handed and cannot stand in for that.
	challenge := sha256.Sum256([]byte(a.verifier))
	// scopes never include c:<workspace>: a login is unscoped and the workspace arrives later
	scopes := scope.Scopes{scope.OpenId, scope.Profile, scope.Email, scope.OfflineAccess}
	result := a.client.AuthorizationEndpoint.Clone()
	result.RawQuery = url.Values{
		"response_type":         {"code"},
		"client_id":             {a.client.CliClientId},
		"redirect_uri":          {a.redirectURI.String()},
		"scope":                 {scopes.String()},
		"state":                 {a.state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}.Encode()
	return result
}

// CheckState reports whether a redirect belongs to this flow. It compares in constant time. That
// is not strictly needed for a value an attacker would have to guess in one shot, and it costs
// nothing to not have to argue about it.
func (a AuthorizationCodeFlow) CheckState(state string) error {
	if subtle.ConstantTimeCompare([]byte(state), []byte(a.state)) != 1 {
		return fmt.Errorf("the login redirect carried the wrong state parameter, so it did not belong to this login")
	}
	return nil
}

// Exchange trades the authorization code for tokens. It is how the verifier and the redirect URI
// reach the token request without a caller having to remember either of them.
func (a AuthorizationCodeFlow) Exchange(ctx context.Context, code string) (Token, error) {
	return a.client.ExchangeAuthCode(ctx, code, a.redirectURI, a.verifier)
}

// randomString is the source of both the PKCE verifier and the state parameter: 32 bytes, which
// is the top of the range RFC 7636 allows for a verifier. crypto/rand.Read fails only when the
// system has no randomness left to give, which is not a condition a login can carry on through.
func randomString() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("cannot read random bytes for the login: %s", err.Error()))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
