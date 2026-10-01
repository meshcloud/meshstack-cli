package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

// CredentialStatus never carries a secret. A part that cannot be read is logged as a warning and
// left at its zero value.
type CredentialStatus struct {
	Profile   profile.Name        `json:"profile"`
	Endpoint  xurl.URL            `json:"endpoint"`
	Kind      credential.Name     `json:"credential"`
	Sources   []string            `json:"sources"`
	Workspace meshstack.Workspace `json:"workspace,omitzero"`
	// Token is read from the cached access token without verifying it, and is nil while none is
	// cached.
	Token *TokenStatus `json:"token,omitzero"`

	// OidcLogin is set for a browser login. ApiKey is set for an API key, and for an API token that
	// an API key minted.
	OidcLogin *OidcLoginStatus `json:"oidcLogin,omitzero"`
	ApiKey    *ApiKeyStatus    `json:"apiKey,omitzero"`

	Unused []UnusedCredential `json:"unusedCredentials,omitzero"`
}

type UnusedCredential struct {
	Kind credential.Name `json:"credential"`
	// Selected is set for the credential the profile selects, which a flag or the environment
	// replaces.
	Selected bool `json:"selected,omitzero"`
	// ClientId is set for an API key.
	ClientId string `json:"clientId,omitzero"`
	// Token is the cached token of a browser login or an API token.
	Token *TokenStatus `json:"token,omitzero"`
}

type TokenStatus struct {
	ExpiresAt   time.Time             `json:"expiresAt,omitzero"`
	User        string                `json:"user,omitzero"`
	Email       string                `json:"email,omitzero"`
	Workspace   meshstack.Workspace   `json:"workspace,omitzero"`
	ClientId    string                `json:"clientId,omitzero"`
	AccessLevel meshstack.AccessLevel `json:"accessLevel,omitzero"`
}

func (t TokenStatus) Expired() bool {
	return !time.Now().Before(t.ExpiresAt)
}

type OidcLoginStatus struct {
	Issuer      xurl.URL              `json:"issuer"`
	AccessLevel meshstack.AccessLevel `json:"accessLevel"`
	// SessionEndsAt is an upper bound, as a logout or an administrator can end the session
	// earlier.
	SessionEndsAt time.Time `json:"sessionEndsAt,omitzero"`
}

func (o OidcLoginStatus) SessionEnded() bool {
	return !o.SessionEndsAt.IsZero() && !time.Now().Before(o.SessionEndsAt)
}

type ApiKeyStatus struct {
	ClientId uuid.UUID      `json:"clientId"`
	Details  *ApiKeyDetails `json:"details,omitzero"`
}

type ApiKeyDetails struct {
	DisplayName      string    `json:"displayName"`
	OwnedByWorkspace string    `json:"ownedByWorkspace"`
	ExpiresAt        time.Time `json:"expiresAt,omitzero"`
	// ExpiresOn is the expiry date, set only by a meshStack that does not send the expiry time
	// yet. It is not turned into a time: the date of an ephemeral key is the day it expires on,
	// not the last day it works.
	ExpiresOn        string                   `json:"expiresOn,omitzero"`
	Permissions      []string                 `json:"permissions"`
	PermissionGroups client.ApiKeyPermissions `json:"-"`
}

const statusTimeout = 10 * time.Second

// Status never renews a browser login, which would rotate its refresh token. Only an API key, or an
// API token it minted, calls meshStack, to read the key's own details.
func (s Session) Status(ctx context.Context) CredentialStatus {
	status := CredentialStatus{
		Profile:  s.CurrentProfile.Name,
		Endpoint: s.CurrentProfile.Endpoint,
		Kind:     s.Credential.Name(),
		Sources:  s.Credential.Sources,
	}
	workspace, err := s.getWorkspace()
	if err != nil && !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		slog.WarnContext(ctx, "Cannot resolve the workspace: "+err.Error())
	}
	status.Workspace = workspace

	var token jwt.JWT
	var found bool
	switch cred := s.Credential.Credential.(type) {
	case *credential.OidcLogin:
		status.OidcLogin = &OidcLoginStatus{Issuer: cred.Issuer, AccessLevel: cmp.Or(cred.AccessLevel, meshstack.AccessFull)}
		if cred.Cache != nil {
			status.OidcLogin.SessionEndsAt = cred.Cache.RefreshExpiresAt
			token, found = oidcTokenFor(cred, workspace)
		}
	case *credential.ApiKey:
		status.ApiKey = &ApiKeyStatus{ClientId: cred.ClientId, Details: apiKeyDetails(ctx, s.Client)}
		token, found = cred.CachedToken(ctx, s.getWorkspace)
	case *credential.Manual:
		token, found = cred.CachedToken(ctx, s.getWorkspace)
		// A pasted API token is most likely the token of a building block run, which its ephemeral
		// API key minted. An expired one would only fail to read the key.
		if clientId, err := uuid.Parse(token.GetClaim(jwt.ClientIdClaim)); err == nil {
			status.ApiKey = &ApiKeyStatus{ClientId: clientId}
			if !tokenStatusOf(token).Expired() {
				status.ApiKey.Details = apiKeyDetails(ctx, s.Client)
			}
		}
	}
	if found {
		status.Token = tokenStatusOf(token)
	}
	status.Unused = s.unusedCredentials(ctx, workspace)
	return status
}

func (s Session) unusedCredentials(ctx context.Context, workspace meshstack.Workspace) (unused []UnusedCredential) {
	stored, err := s.CurrentProfile.Credentials(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Cannot read the stored credentials: "+err.Error())
		return nil
	}
	for _, name := range credential.Names {
		cred := stored.ByName(name)
		if cred == nil || name == s.Credential.Name() && s.Credential.Stored {
			continue
		}
		if err := CacheFor(s.CurrentProfile, cred).Load(ctx); err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Cannot read the cached token of the stored %s: %s", name.Label(), err))
		}
		entry := UnusedCredential{Kind: name, Selected: name == s.CurrentProfile.Credential}
		switch cred := cred.(type) {
		case *credential.ApiKey:
			entry.ClientId = cred.ClientId.String()
		case *credential.OidcLogin:
			if token, found := oidcTokenFor(cred, workspace); found {
				entry.Token = tokenStatusOf(token)
			}
		case *credential.Manual:
			if token, found := cred.CachedToken(ctx, nil); found {
				entry.Token = tokenStatusOf(token)
			}
		}
		unused = append(unused, entry)
	}
	return unused
}

func oidcTokenFor(cred *credential.OidcLogin, workspace meshstack.Workspace) (jwt.JWT, bool) {
	if cred.Cache == nil {
		return jwt.JWT{}, false
	}
	for _, token := range cred.Cache.ScopedTokens {
		if token.GetClaim(jwt.WorkspaceClaim) == workspace {
			return token, true
		}
	}
	tokens := slices.SortedFunc(maps.Values(cred.Cache.ScopedTokens), func(a, b jwt.JWT) int {
		return b.GetClaim(jwt.ExpiryClaim).Compare(a.GetClaim(jwt.ExpiryClaim).Time)
	})
	if len(tokens) == 0 {
		return jwt.JWT{}, false
	}
	return tokens[0], true
}

func tokenStatusOf(token jwt.JWT) *TokenStatus {
	level, _ := meshstack.AccessLevelOf(token.GetClaim(jwt.ScopeClaim))
	return &TokenStatus{
		ExpiresAt:   token.GetClaim(jwt.ExpiryClaim).Time,
		User:        token.GetClaim(jwt.PreferredUsernameClaim),
		Email:       token.GetClaim(jwt.EmailClaim),
		Workspace:   token.GetClaim(jwt.WorkspaceClaim),
		ClientId:    token.GetClaim(jwt.ClientIdClaim),
		AccessLevel: level,
	}
}

func apiKeyDetails(ctx context.Context, newClient func() (client.Client, error)) *ApiKeyDetails {
	c, err := newClient()
	if err != nil {
		slog.WarnContext(ctx, "Cannot read the details of the API key: "+err.Error())
		return nil
	}
	readCtx, cancel := context.WithTimeoutCause(ctx, statusTimeout,
		fmt.Errorf("meshStack did not answer within %s", statusTimeout))
	defer cancel()
	apiKey, err := c.ApiKey.ReadSelf(readCtx)
	if cause := context.Cause(readCtx); err != nil && cause != nil {
		err = cause
	}
	switch httpErr, isHttpErr := errors.AsType[http.Error](err); {
	// A meshStack without /self takes it for the uuid of a key: without APIKEY_LIST that is a 403,
	// and with it a 404, which ReadSelf returns as nil.
	case isHttpErr && httpErr.IsForbidden(), err == nil && apiKey == nil:
		slog.WarnContext(ctx, "Cannot read the details of the API key: this meshStack does not let an API key read itself yet")
		return nil
	case err != nil:
		slog.WarnContext(ctx, "Cannot read the details of the API key: "+err.Error())
		return nil
	}
	codes := make([]string, 0, len(apiKey.Spec.Permissions))
	for _, code := range apiKey.Spec.Permissions {
		codes = append(codes, string(code))
	}
	slices.Sort(codes)
	details := &ApiKeyDetails{
		DisplayName:      apiKey.Spec.DisplayName,
		OwnedByWorkspace: apiKey.Metadata.OwnedByWorkspace,
		Permissions:      codes,
		PermissionGroups: client.Permissions.Only(apiKey.Spec.Permissions),
	}
	if apiKey.Status != nil {
		details.ExpiresAt = apiKey.Status.ExpiresAt
	}
	if details.ExpiresAt.IsZero() && apiKey.Spec.ExpiresAt != nil {
		details.ExpiresOn = *apiKey.Spec.ExpiresAt
	}
	return details
}
