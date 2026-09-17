package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/browser"
)

// resolveOidcLoginCredential logs a person in through a browser, which is why
// Session.credentialResolvers offers it only to a caller that named it.
func (s Session) resolveOidcLoginCredential(ctx context.Context, opts ResolveSessionOptions) (credential.Credential, error) {
	meshInfo, err := s.CheckedMeshInfo()
	if err != nil {
		return nil, err
	}
	oidcClient, err := oidc.NewClient(ctx, s.HttpClient, meshInfo.Issuer, meshInfo.CliClientId)
	if err != nil {
		return nil, err
	}
	token, err := browser.Login(ctx, oidcClient)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, fmt.Sprintf("Logged in at %s through a browser", oidcClient.Issuer))
	oidcLogin := &credential.OidcLogin{Endpoint: s.Endpoint, Issuer: oidcClient.Issuer, ClientId: oidcClient.Id}
	oidcLogin.StoreLogin(token.RefreshToken, token.AccessToken)

	return oidcLogin, nil
}
