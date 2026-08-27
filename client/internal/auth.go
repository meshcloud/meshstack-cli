package internal

import (
	"context"
	"fmt"
)

// Authorization produces the Authorization request header for each request, renewing the
// token it holds whenever the token is close to expiry.
//
// Renewal itself is deliberately not here. This file used to post to /api/login and cache
// the resulting token in memory only, which is exactly why it could not stay: it had no way
// to write the minted token into a profile, so every CLI invocation and every Terraform
// provider run re-minted one. pkg/auth is now the single place that mints, caches and
// persists, for all three authentication methods — and because the header no longer needs an
// HttpClient to produce it, an Authorization can finally be implemented from outside client/.
type Authorization interface {
	Header(ctx context.Context) (string, error)
}

// TokenRejector is implemented by an Authorization that can be told the header it produced
// came back 401, so that DoAuthorizedRequest can force exactly one re-mint before the error
// surfaces.
//
// The renewal grace window covers a request issued just before expiry and modest clock skew,
// but not a clock that is minutes wrong — which containers with a frozen clock really are. One
// bounded retry turns that from a confusing failure into a hiccup. It is an optional interface
// rather than part of Authorization because a static bearer token has nothing to re-mint.
type TokenRejector interface {
	// Rejected reports that header was refused. The header is passed so that an
	// implementation can ignore a report about a token it has already replaced.
	Rejected(header string)
}

// BearerTokenAuthorization carries a token somebody else obtained. Nothing renews it.
type BearerTokenAuthorization struct {
	Token string
}

func (auth BearerTokenAuthorization) Header(context.Context) (string, error) {
	return fmt.Sprintf("Bearer %s", auth.Token), nil
}
