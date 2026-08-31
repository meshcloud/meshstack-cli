package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/url"
)

// scopes never include c:<workspace>: a login is unscoped and the workspace arrives later,
// through a refresh grant. offline_access is an optional client scope, so it has to be asked
// for by name or the login lasts as long as one access token.
const scopes = "openid profile email offline_access"

// AuthorizationCodeStarter is what an interactive front end is handed instead of a Client. It
// is an interface so that the browser login is *given* the protocol rather than reaching for
// it: every part that has to be right — the authorization parameters, PKCE, the state
// comparison, the token request — stays in this package, and pkg/oidc/browser is left with a
// loopback listener, a way to open a browser, and a page to show when the redirect lands.
//
// pkg/auth is where the two halves are wired together. It holds the discovered Client and
// passes it as this interface to the Browser it got from Input.
type AuthorizationCodeStarter interface {
	// NewAuthorizationCode begins a flow that will redirect to redirectURI. The caller binds
	// its listener first, because the port is part of the URI and the token request has to
	// echo the URI back unchanged.
	NewAuthorizationCode(redirectURI string) (AuthorizationCodeFlow, error)
}

// AuthorizationCodeFlow is one run of the authorization code flow with PKCE. It holds the two
// values that must survive from the authorization request to the token request and that must
// not leave this package: the PKCE verifier and the state parameter.
type AuthorizationCodeFlow interface {
	// URL is where the person has to go.
	URL() string

	// CheckState reports whether a redirect belongs to this flow.
	CheckState(state string) error

	// Exchange trades the authorization code for tokens.
	Exchange(ctx context.Context, code string) (Token, error)
}

var _ AuthorizationCodeStarter = Client{}

func (c Client) NewAuthorizationCode(redirectURI string) (AuthorizationCodeFlow, error) {
	verifier, err := randomString()
	if err != nil {
		return nil, err
	}
	state, err := randomString()
	if err != nil {
		return nil, err
	}
	return &authorizationCode{client: c, redirectURI: redirectURI, verifier: verifier, state: state}, nil
}

type authorizationCode struct {
	client      Client
	redirectURI string
	verifier    string
	state       string
}

func (a *authorizationCode) URL() string {
	challenge := sha256.Sum256([]byte(a.verifier))
	authURL := *a.client.AuthorizationEndpoint.URL
	authURL.RawQuery = url.Values{
		"response_type":         {"code"},
		"client_id":             {a.client.CliClientId},
		"redirect_uri":          {a.redirectURI},
		"scope":                 {scopes},
		"state":                 {a.state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}.Encode()
	return authURL.String()
}

// CheckState compares in constant time. That is not strictly needed for a value an attacker
// would have to guess in one shot, and it costs nothing to not have to argue about it.
func (a *authorizationCode) CheckState(state string) error {
	if subtle.ConstantTimeCompare([]byte(state), []byte(a.state)) != 1 {
		return fmt.Errorf("the login redirect carried the wrong state parameter, so it did not belong to this login")
	}
	return nil
}

func (a *authorizationCode) Exchange(ctx context.Context, code string) (Token, error) {
	return a.client.exchange(ctx, code, a.redirectURI, a.verifier)
}

// randomString is the source of both the PKCE verifier and the state parameter: 32 bytes,
// which is the top of the range RFC 7636 allows for a verifier.
func randomString() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot read random bytes for the login: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
