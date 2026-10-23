package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/browser"
)

func (s Session) resolveOidcLoginCredential(ctx context.Context, previous *credential.OidcLogin) (Credential, error) {
	meshInfo, err := s.MeshInfo()
	if err != nil {
		return Credential{}, err
	}
	oidcClient, err := oidc.NewClient(ctx, s.httpClient, meshInfo.Issuer, meshInfo.CliClientId)
	if err != nil {
		return Credential{}, err
	}
	var replaced browser.Previous
	if previous != nil {
		replaced.Level = previous.AccessLevel
		if err = CacheFor(s.CurrentProfile, previous).Load(ctx); err != nil {
			return Credential{}, err
		}
		if previous.Cache != nil {
			replaced.RefreshToken = previous.Cache.RefreshToken
		}
	}
	token, level, err := browser.Login(ctx, oidcClient, replaced)
	if err != nil {
		return Credential{}, err
	}
	slog.DebugContext(ctx, fmt.Sprintf("Logged in at %s through a browser with access level %q", oidcClient.Issuer, level))
	oidcLogin := &credential.OidcLogin{
		Endpoint:    s.CurrentProfile.Endpoint,
		Issuer:      oidcClient.Issuer,
		ClientId:    oidcClient.Id,
		AccessLevel: level,
	}
	oidcLogin.StoreLogin(token)

	return CacheFor(s.CurrentProfile, oidcLogin), nil
}
