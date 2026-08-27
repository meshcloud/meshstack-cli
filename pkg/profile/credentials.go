package profile

import (
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// Credentials is `credentials/<profile>.yaml`: how one profile authenticates, and the
// access tokens it has minted so far.
//
// The endpoint sits at the top of the file rather than on each method, because a valid
// cached token is used without ever consulting a method: a per-method check would be
// skipped exactly when the CLI is about to send a stored bearer token to whatever
// endpoint the profile now names.
type Credentials struct {
	Version       int                             `yaml:"version"`
	Endpoint      string                          `yaml:"endpoint"`
	CurrentMethod method.Method                   `yaml:"currentMethod,omitempty"`
	Methods       Methods                         `yaml:"methods,omitempty"`
	AccessTokens  map[workspace.Scope]IssuedToken `yaml:"accessTokens,omitempty"`
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
	Issuer       string    `yaml:"issuer"`
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
	Token     string    `yaml:"token"`
	ExpiresAt time.Time `yaml:"expiresAt"`
}

// prune drops access tokens that have expired, which is what keeps the map from growing
// by one entry per workspace ever used. An empty map is returned as nil so that
// `omitempty` leaves the key out of the file entirely.
func prune(tokens map[workspace.Scope]IssuedToken, now time.Time) map[workspace.Scope]IssuedToken {
	if len(tokens) == 0 {
		return nil
	}
	kept := make(map[workspace.Scope]IssuedToken, len(tokens))
	for scope, token := range tokens {
		if token.ExpiresAt.After(now) {
			kept[scope] = token
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
