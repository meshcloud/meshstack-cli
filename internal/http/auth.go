package http

import (
	"context"
)

// Authorization produces the bearer token for each request, renewing the token it holds
// whenever the token is close to expiry.
//
// Minting is deliberately not here. This file used to post to /api/login and cache the
// resulting token in memory only, which is exactly why it could not stay: it had no way to
// write the minted token into a profile, so every CLI invocation and every Terraform provider
// run re-minted one. pkg/auth is now the single place that mints, caches and persists, for all
// three authentication methods — and because a token no longer needs an Client to produce
// it, an Authorization can finally be implemented from outside client/.
type Authorization interface {
	// BearerToken returns the token to send, without the "Bearer " prefix.
	BearerToken(ctx context.Context) (string, error)

	// RefreshBearerToken replaces rejected, which the meshStack API answered with a 401, and
	// returns the token to retry that one request with. DoAuthorizedRequest calls it at most
	// once per request; returning rejected unchanged means there is nothing new to try, and
	// then the 401 is what the caller sees.
	//
	// rejected is passed rather than implied because a Terraform provider has many requests in
	// flight at once: several of them can be refused the same token, and only the first report
	// should cost a new one. An implementation compares rejected with the token it now holds
	// and hands back the replacement it already has.
	RefreshBearerToken(ctx context.Context, rejected string) (string, error)
}

// BearerTokenAuthorization carries a token somebody else obtained. Nothing renews it.
type BearerTokenAuthorization struct {
	Token string
}

func (auth BearerTokenAuthorization) BearerToken(context.Context) (string, error) {
	return auth.Token, nil
}

// RefreshBearerToken hands the same token back: a static bearer token has nothing to re-mint,
// so a 401 on it is the answer the caller gets.
func (auth BearerTokenAuthorization) RefreshBearerToken(_ context.Context, rejected string) (string, error) {
	return rejected, nil
}
