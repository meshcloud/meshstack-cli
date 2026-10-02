package credential

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"time"

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
	// AccessLevel is empty for a login of an older CLI, or on a meshStack without access levels.
	AccessLevel meshstack.AccessLevel `json:"accessLevel,omitzero"`
	Cache       *struct {
		RefreshToken     string                  `json:"refreshToken"`
		RefreshExpiresAt time.Time               `json:"refreshExpiresAt,omitzero"`
		ScopedTokens     map[scope.Scope]jwt.JWT `json:"tokens,omitzero"`
	} `json:"-"`
}

func (oidcLogin *OidcLogin) Name() Name {
	return OidcLoginName
}

func (oidcLogin *OidcLogin) Identity() Identity {
	return identityOf(oidcLogin)
}

func (oidcLogin *OidcLogin) StoreLogin(oidcToken oidc.Token) {
	if oidcLogin.Cache == nil {
		newCache(oidcLogin)
	}
	oidcLogin.Cache.RefreshToken = oidcToken.RefreshToken
	oidcLogin.Cache.RefreshExpiresAt = oidcToken.RefreshExpiresAt
	if oidcLogin.Cache.ScopedTokens == nil {
		oidcLogin.Cache.ScopedTokens = map[scope.Scope]jwt.JWT{}
	}
	// An initial login carries no workspace claim, and neither does a token keycloak minted for a
	// workspace it refused, so both are stored as the unscoped token they are.
	token := oidcToken.AccessToken
	oidcLogin.Cache.ScopedTokens[tokenCacheKey(token.GetClaim(jwt.WorkspaceClaim))] = token
}

func (oidcLogin *OidcLogin) CachedToken(ctx context.Context, getWorkspace getWorkspaceFunc) (token jwt.JWT, found bool) {
	if oidcLogin.Cache == nil {
		return
	}
	if workspace, err := getWorkspace(); err != nil {
		// Debug, not Warn: RefreshCachedToken hits the same error and returns it to the caller.
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
	if noWorkspace, ok := errors.AsType[meshstack.NoWorkspaceToWorkInError](err); ok {
		// The setting resolution around it only adds the sources a workspace could come from, and
		// noWorkspace already says which of them helps.
		return noWorkspace
	} else if err != nil {
		return fmt.Errorf("a browser login needs a workspace; name one, or give the profile a default workspace with 'meshstack profile edit' or 'meshstack login --endpoint %s': %w", oidcLogin.Endpoint, err)
	}
	oidcClient, err := oidc.NewClient(ctx, client, oidcLogin.Issuer, oidcLogin.ClientId)
	if err != nil {
		return err
	}
	oidcToken, err := oidcClient.Refresh(ctx, oidcLogin.Cache.RefreshToken, scopesFor(workspace))
	// OAuth 2.0 answers the refresh token of an ended session with 400 invalid_grant (RFC 6749,
	// section 5.2).
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.StatusCode == gohttp.StatusBadRequest {
		return fmt.Errorf("the browser login at %s cannot be renewed, its session has most likely ended; run 'meshstack login --endpoint %s' again: %w",
			oidcLogin.Issuer, oidcLogin.Endpoint, err)
	} else if err != nil {
		return err
	}
	// Stored before the check below, so that the rotated refresh token is kept even when the
	// workspace turns out to be wrong. Keycloak ends the whole session when a session replays
	// a refresh token it has already rotated away.
	oidcLogin.StoreLogin(oidcToken)
	if workspaceFromToken := oidcToken.AccessToken.GetClaim(jwt.WorkspaceClaim); workspace != meshstack.NoWorkspace && workspaceFromToken != workspace {
		slog.DebugContext(ctx, fmt.Sprintf("%s minted a token for workspace %s, not for %s", oidcLogin.Issuer, workspaceFromToken, workspace))
		return meshstack.NoRoleInWorkspaceError{Workspace: workspace}
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
// session note, while any other non-c: scope clears that note. Asking for no workspace therefore
// names offline_access, where openid alone would inherit the previous workspace.
//
// The access level needs no scope here: keycloak keeps the scopes granted at login on every
// refresh, whatever the request names.
func scopesFor(workspace meshstack.Workspace) scope.Scopes {
	if workspace == meshstack.NoWorkspace {
		return scope.Scopes{scope.OpenId, scope.OfflineAccess}
	}
	return scope.Scopes{scope.OpenId, workspaceScope(workspace)}
}
