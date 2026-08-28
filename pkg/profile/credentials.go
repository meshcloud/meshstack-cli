package profile

import (
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// Credentials is `credentials/<profile>.yaml`: how one profile authenticates, and the
// access tokens it has minted so far.
//
// The endpoint sits at the top of the file rather than on each method, because a valid
// cached token is used without ever consulting a method: a per-method check would be
// skipped exactly when the CLI is about to send a stored bearer token to whatever
// endpoint the profile now names.
type Credentials struct {
	Version int `yaml:"version"`
	// A pointer, because the file of a profile nothing has logged in to yet carries no
	// endpoint, and an xurl.URL that is present has been parsed.
	Endpoint      *xurl.URL                   `yaml:"endpoint,omitempty"`
	CurrentMethod method.Method               `yaml:"currentMethod,omitempty"`
	Methods       Methods                     `yaml:"methods,omitempty"`
	AccessTokens  map[scope.Scope]IssuedToken `yaml:"accessTokens,omitempty"`
}

// Methods is a mapping keyed by method name rather than a list with a discriminator, so
// that presence is a pointer, a duplicate is unrepresentable, and no custom unmarshaler
// is needed. There is no entry for method.Manual: a pasted token needs nothing to renew
// it, which is why it cannot be renewed.
type Methods struct {
	Login  *LoginMethod  `yaml:"login,omitempty"`
	ApiKey *ApiKeyMethod `yaml:"apiKey,omitempty"`
}

// LoginMethod holds the browser login. ObtainedAt is recorded instead of a predicted
// deadline: the session's ceiling is a server-side constant, so a number copied into
// the CLI would be wrong wherever a realm configures another one.
type LoginMethod struct {
	Issuer       *xurl.URL `yaml:"issuer,omitempty"`
	RefreshToken string    `yaml:"refreshToken"`
	ObtainedAt   time.Time `yaml:"obtainedAt"`
}

// ApiKeyMethod holds an API key. ClientSecret is absent when ClientSecretCommand is
// set, so a long-lived secret never has to sit on disk.
type ApiKeyMethod struct {
	ClientId            string   `yaml:"clientId"`
	ClientSecret        string   `yaml:"clientSecret,omitempty"`
	ClientSecretCommand []string `yaml:"clientSecretCommand,omitempty"`
}

// IssuedToken is one cached access token, keyed by the workspace scope it carries.
type IssuedToken struct {
	Token     jwt.JWT   `yaml:"token"`
	ExpiresAt time.Time `yaml:"expiresAt"`
}

// prune drops access tokens that have expired, which is what keeps the map from growing
// by one entry per workspace ever used. An empty map is returned as nil so that
// `omitempty` leaves the key out of the file entirely.
//
// A zero ExpiresAt is kept, because it means "this token said nothing about its own life"
// rather than "expired at the zero time". An API token that is not a JWT is the case:
// `meshstack auth login --api-token` stores one with an unknown expiry rather than a guessed
// one, and dropping it here would make the very next command report it as expired.
func prune(tokens map[scope.Scope]IssuedToken, now time.Time) map[scope.Scope]IssuedToken {
	if len(tokens) == 0 {
		return nil
	}
	kept := make(map[scope.Scope]IssuedToken, len(tokens))
	for key, token := range tokens {
		if token.ExpiresAt.IsZero() || token.ExpiresAt.After(now) {
			kept[key] = token
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
