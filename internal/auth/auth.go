package auth

import (
	"context"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ http.Authorization = Session{}

func (s Session) GetBearerToken(ctx context.Context) (out http.BearerToken, err error) {
	return s.RefreshBearerToken(ctx, "")
}

func (s Session) RefreshBearerToken(ctx context.Context, rejected http.BearerToken) (out http.BearerToken, err error) {
	// Resolved before any cache lock is taken: resolving the workspace may list workspaces, and
	// that request needs a token of its own, which takes the same lock. A failure is returned by
	// the calls below, which get the memoized result.
	_, _ = s.getWorkspace()
	usable := func() bool {
		token, found := s.Credential.CachedToken(ctx, s.getWorkspace)
		if !found || token.GetClaim(jwt.ExpiryClaim).Expired(30*time.Second) || token.String() == string(rejected) {
			return false
		}
		out = http.BearerToken(token.String())
		return true
	}

	var foundCached bool
	err = s.Credential.Read(ctx, func() error {
		foundCached = usable()
		return nil
	})
	if err != nil || foundCached {
		return
	}
	err = s.Credential.Modify(ctx, func() error {
		if usable() {
			return nil
		}
		// Not cancelled with ctx, so that Ctrl+C or a short deadline cannot cut a refresh off after the
		// issuer has rotated the refresh token: the rotated one would be lost, and with it the login.
		// Modify writes it to the cache before it returns, and the HTTP client's timeouts still end it.
		if err := s.Credential.RefreshCachedToken(context.WithoutCancel(ctx), s.httpClient, s.getWorkspace); err != nil {
			return err
		}
		// RefreshCachedToken guarantees a cached token, so found is always true here.
		token, _ := s.Credential.CachedToken(ctx, s.getWorkspace)
		out = http.BearerToken(token.String())
		return nil
	})
	return
}
