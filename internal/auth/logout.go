package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

// Logout removes the stored credentials of p, and first ends the session of its browser login.
func Logout(ctx context.Context, p *profile.Profile, opts ResolveSessionOptions) error {
	EndBrowserSession(ctx, p, opts)
	return p.RemoveCredentials(ctx)
}

// EndBrowserSession ends the session of p's browser login at the identity provider, where p stores
// one. A session that cannot be ended only gets a warning, since it expires on its own.
func EndBrowserSession(ctx context.Context, p *profile.Profile, opts ResolveSessionOptions) {
	creds, err := p.Credentials(ctx)
	if err == nil && creds.OidcLogin != nil {
		err = endSession(ctx, p, creds.OidcLogin, opts)
	}
	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cannot end the session of the browser login of profile '%s', so it stays valid until it expires: %s", p, err))
	}
}

func endSession(ctx context.Context, p *profile.Profile, oidcLogin *credential.OidcLogin, opts ResolveSessionOptions) error {
	if err := CacheFor(p, oidcLogin).Load(ctx); err != nil {
		return err
	}
	if oidcLogin.Cache == nil || oidcLogin.Cache.RefreshToken == "" {
		return errors.New("its refresh token is not cached")
	}
	httpClient, err := opts.HttpClient()
	if err != nil {
		return err
	}
	oidcClient, err := oidc.NewClient(ctx, httpClient, oidcLogin.Issuer, oidcLogin.ClientId)
	if err != nil {
		return err
	}
	return oidcClient.RevokeRefreshToken(ctx, oidcLogin.Cache.RefreshToken)
}
