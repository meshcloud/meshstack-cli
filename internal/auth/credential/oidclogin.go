package credential

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/oidc/scope"
)

var _ Credential = &OidcLogin{}

type OidcLogin struct {
	Endpoint xurl.URL `json:"endpoint"`
	Issuer   xurl.URL `json:"issuer"`
	ClientId string   `json:"clientId"`
	Cache    *struct {
		RefreshToken string                  `json:"refreshToken"`
		ScopedTokens map[scope.Scope]jwt.JWT `json:"tokens,omitzero"`
	} `json:"-"`
}

func (oidcLogin *OidcLogin) Identity() Identity {
	return identityOf(oidcLogin)
}

func (oidcLogin *OidcLogin) StoreLogin(refreshToken string, token jwt.JWT) {
	if oidcLogin.Cache == nil {
		newCache(oidcLogin)
	}
	oidcLogin.Cache.RefreshToken = refreshToken
	if oidcLogin.Cache.ScopedTokens == nil {
		oidcLogin.Cache.ScopedTokens = map[scope.Scope]jwt.JWT{}
	}
	oidcLogin.Cache.ScopedTokens[meshstack.WorkspaceFromToken(token).AsScope()] = token
}

func (oidcLogin *OidcLogin) CachedToken(_ context.Context, workspace meshstack.Workspace) (token jwt.JWT, found bool) {
	if oidcLogin.Cache == nil {
		return
	}
	token, found = oidcLogin.Cache.ScopedTokens[workspace.AsScope()]
	return
}

func (oidcLogin *OidcLogin) RefreshCachedToken(ctx context.Context, client http.Client, workspace meshstack.Workspace) error {
	if oidcLogin.Cache == nil || oidcLogin.Cache.RefreshToken == "" {
		return fmt.Errorf("no login is cached for %s; run 'meshstack login --endpoint %s'", oidcLogin.Endpoint, oidcLogin.Endpoint)
	}
	oidcClient, err := oidc.NewClient(ctx, client, oidcLogin.Issuer, oidcLogin.ClientId)
	if err != nil {
		return err
	}
	// Verified against a live keycloak: the script mapper in
	// ../meshfed-release/keycloak/container/MC_CUSTOMER.js ignores the default scopes openid,
	// profile and email and falls back to the workspace in a keycloak session note when nothing
	// else is left, while any other non-c: scope clears that note. So asking for no workspace
	// names offline_access to clear it, where openid alone would inherit the previous workspace.
	scopes := scope.Scopes{scope.OpenId, scope.OfflineAccess}
	if workspace != meshstack.NoWorkspace {
		scopes = scope.Scopes{scope.OpenId, workspace.AsScope()}
	}
	token, err := oidcClient.Refresh(ctx, oidcLogin.Cache.RefreshToken, scopes)
	if err != nil {
		return err
	}
	// Storing before the check keeps the rotated refresh token, which stays valid.
	// Important to not break the refresh token lineage (Keycloak logs out sessions which replay an old refresh token)!
	oidcLogin.StoreLogin(token.RefreshToken, token.AccessToken)
	if workspaceFromToken := meshstack.WorkspaceFromToken(token.AccessToken); workspace != meshstack.NoWorkspace && workspaceFromToken != workspace {
		return fmt.Errorf("no access to workspace '%s': %s minted a token for workspace '%s' instead; check the workspace identifier, or ask for access to it in meshPanel",
			workspace, oidcLogin.Issuer, workspaceFromToken)
	}
	return nil
}
