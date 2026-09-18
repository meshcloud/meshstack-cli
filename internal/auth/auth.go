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
	usable := func() bool {
		token, found := s.Credential.CachedToken(ctx, s.getWorkspace)
		if !found || token.GetClaim(jwt.ExpiryClaim).Expired(30*time.Second) || token.String() == string(rejected) {
			return false
		}
		out = http.BearerToken(token.String())
		return true
	}

	var foundCached bool
	err = s.Credentials.ReadCache(ctx, s.Credential, func() error {
		foundCached = usable()
		return nil
	})
	if err != nil || foundCached {
		return
	}
	err = s.Credentials.ModifyCache(ctx, s.Credential, func() error {
		if usable() {
			return nil
		}
		if err := s.Credential.RefreshCachedToken(ctx, s.httpClient, s.getWorkspace); err != nil {
			return err
		}
		// RefreshCachedToken guarantees a cached token, so found is always true here.
		token, _ := s.Credential.CachedToken(ctx, s.getWorkspace)
		out = http.BearerToken(token.String())
		return nil
	})
	return
}
