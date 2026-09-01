package profile

import (
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// Credentials is `credentials/<profile>.json`: how one profile authenticates, and the
// access tokens it has minted so far.
//
// The endpoint sits at the top of the file rather than on each method, because a valid
// cached token is used without ever consulting a method: a per-method check would be
// skipped exactly when the CLI is about to send a stored bearer token to whatever
// endpoint the profile now names.
type Credentials struct {
	Version int `json:"version"`
	// A pointer, because the file of a profile nothing has logged in to yet carries no
	// endpoint, and an xurl.URL that is present has been parsed.
	Endpoint *xurl.URL `json:"endpoint,omitzero"`
	credential.Credential
}

// prune drops access tokens that have expired, from whichever of the three shapes the
// credential holds. It is what keeps a login's map from growing by one entry per workspace
// ever used.
func prune(c credential.Credential, now time.Time) credential.Credential {
	if c.Login != nil {
		login := *c.Login
		login.AccessTokens = pruneTokens(login.AccessTokens, now)
		c.Login = &login
	}
	if c.ApiKey != nil && expired(c.ApiKey.AccessToken, now) {
		apiKey := *c.ApiKey
		apiKey.AccessToken = credential.IssuedToken{}
		c.ApiKey = &apiKey
	}
	if c.Manual != nil && expired(c.Manual.AccessToken, now) {
		manual := *c.Manual
		manual.AccessToken = credential.IssuedToken{}
		c.Manual = &manual
	}
	return c
}

func pruneTokens(tokens map[scope.Scope]credential.IssuedToken, now time.Time) map[scope.Scope]credential.IssuedToken {
	if len(tokens) == 0 {
		return nil
	}
	kept := make(map[scope.Scope]credential.IssuedToken, len(tokens))
	for key, token := range tokens {
		if !expired(token, now) {
			kept[key] = token
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// expired is false for a zero ExpiresAt, which means "this token said nothing about its own
// life" rather than "expired at the zero time". An API token that is not a JWT is the case:
// `meshstack auth login --api-token` stores one with an unknown expiry rather than a guessed
// one, and dropping it here would make the very next command report it as expired.
func expired(token credential.IssuedToken, now time.Time) bool {
	return !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(now)
}
