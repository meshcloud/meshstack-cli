package credential

import (
	"context"
	"fmt"
	"log/slog"

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
	// An initial login carries no workspace claim, and neither does a token keycloak minted for a
	// workspace it refused, so both are stored as the unscoped token they are.
	oidcLogin.Cache.ScopedTokens[tokenCacheKey(token.GetClaim(jwt.WorkspaceClaim))] = token
}

func (oidcLogin *OidcLogin) CachedToken(ctx context.Context, getWorkspace getWorkspaceFunc) (token jwt.JWT, found bool) {
	if oidcLogin.Cache == nil {
		return
	}
	if workspace, err := getWorkspace(); err != nil {
		// this will most likely propagate to RefreshCachedToken and thus will bubble up the error,
		// so Debug log is fine here.
		slog.DebugContext(ctx, fmt.Sprintf("Cannot obtain cached token for %s at %s without workspace: %s", OidcLoginName, oidcLogin.Endpoint, err.Error()))
		return
	} else {
		token, found = oidcLogin.Cache.ScopedTokens[tokenCacheKey(workspace)]
	}
	return
}

func (oidcLogin *OidcLogin) RefreshCachedToken(ctx context.Context, client http.Client, getWorkspace getWorkspaceFunc) error {
	if oidcLogin.Cache == nil || oidcLogin.Cache.RefreshToken == "" {
		return fmt.Errorf("no refresh token available for %T; run 'meshstack login --endpoint %s'", oidcLogin, oidcLogin.Endpoint)
	}
	workspace, err := getWorkspace()
	if err != nil {
		// TODO profile default workspace can't set otherwise as long as 'meshstack profile edit' is missing (there's no profile CRUD in CLI at all right now)
		return fmt.Errorf("a workspace is required for %T; configure one or run 'meshstack login --endpoint %s' and pick one as profile default", oidcLogin, oidcLogin.Endpoint)
	}
	oidcClient, err := oidc.NewClient(ctx, client, oidcLogin.Issuer, oidcLogin.ClientId)
	if err != nil {
		return err
	}
	oidcToken, err := oidcClient.Refresh(ctx, oidcLogin.Cache.RefreshToken, scopesFor(workspace))
	if err != nil {
		return err
	}
	// Storing before the check keeps the rotated refresh token, which stays valid.
	// Important to not break the refresh token lineage (Keycloak logs out sessions which replay an old refresh token)!
	oidcLogin.StoreLogin(oidcToken.RefreshToken, oidcToken.AccessToken)
	if workspaceFromToken := oidcToken.AccessToken.GetClaim(jwt.WorkspaceClaim); workspace != meshstack.NoWorkspace && workspaceFromToken != workspace {
		return fmt.Errorf("no access to workspace '%s': %s minted a token for workspace '%s' instead; check the workspace identifier, or ask for access to it in meshPanel",
			workspace, oidcLogin.Issuer, workspaceFromToken)
	}
	return nil
}

// workspaceScope binds a token to one workspace. Keycloak's mapper strips this prefix again before
// it writes the identifier into the claim, see jwt.WorkspaceClaim.
func workspaceScope(workspace meshstack.Workspace) scope.Scope {
	return "c:" + scope.Scope(workspace)
}

// tokenCacheKey is what a token is stored under in OidcLogin.Cache.ScopedTokens, so these values
// are on disk and cannot change freely.
func tokenCacheKey(workspace meshstack.Workspace) scope.Scope {
	if workspace == meshstack.NoWorkspace {
		return "unscoped"
	}
	return workspaceScope(workspace)
}

// scopesFor asks for a token bound to one workspace, or for one bound to none. Verified against a
// live keycloak: the script mapper in ../meshfed-release/keycloak/container/MC_CUSTOMER.js ignores
// the default scopes openid, profile and email and falls back to the workspace in a keycloak
// session note when nothing else is left, while any other non-c: scope clears that note. So asking
// for no workspace names offline_access to clear it, where openid alone would inherit the previous
// workspace.
func scopesFor(workspace meshstack.Workspace) scope.Scopes {
	if workspace == meshstack.NoWorkspace {
		return scope.Scopes{scope.OpenId, scope.OfflineAccess}
	}
	return scope.Scopes{scope.OpenId, workspaceScope(workspace)}
}
