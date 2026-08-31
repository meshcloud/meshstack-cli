// Package credential says how a caller authenticates against the meshStack API.
//
// It is top level rather than a subpackage of either package that uses it. A credential from
// the environment or a Terraform provider block touches no file, so pkg/profile does not own
// it; and pkg/profile imports it, so it cannot sit under pkg/auth.
//
// The method strings and the json tags are part of the on-disk format. Renaming one breaks
// every credentials file already written.
package credential

import (
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// Method's constants carry the prefix because the plain names are taken by the shapes below.
type Method string

const (
	MethodLogin  Method = "login"
	MethodApiKey Method = "apiKey"
	MethodManual Method = "manual"
)

// Description names the method the way a message to a user should, because "apiKey"
// on its own reads like a field name rather than a thing the reader has.
func (m Method) Description() string {
	switch m {
	case MethodLogin:
		return "browser login"
	case MethodApiKey:
		return "API key"
	case MethodManual:
		return "API token"
	default:
		return string(m)
	}
}

// Credential holds more than one method at a time, so that `meshstack login` switches back
// from an API key without asking for the id again.
//
// Presence is not selection: switch on Current, never on a nil check. A profile that mints
// with its API key may still hold a browser login, and `if c.Login != nil` would wrongly
// demand a workspace of it.
type Credential struct {
	Current Method  `json:"current,omitzero"`
	Login   *Login  `json:"login,omitzero"`
	ApiKey  *ApiKey `json:"apiKey,omitzero"`
	Manual  *Manual `json:"manual,omitzero"`
}

// Login records ObtainedAt instead of a predicted deadline: the session's ceiling is a
// server-side constant, so a number copied into the CLI would be wrong wherever a realm
// configures another one.
//
// It is the one method whose tokens are a map, because a browser login mints a token bound to
// one workspace: one per `c:` scope, plus the unscoped one that lists the workspaces.
type Login struct {
	Issuer       *xurl.URL `json:"issuer,omitzero"`
	RefreshToken string    `json:"refreshToken,omitzero"`
	ObtainedAt   time.Time `json:"obtainedAt,omitzero"`

	AccessTokens map[scope.Scope]IssuedToken `json:"accessTokens,omitzero"`
}

// ApiKey leaves Secret absent when SecretCommand is set, so a long-lived secret never has to
// sit on disk. AccessToken is the unscoped token: an API key carries whatever workspace its
// issuer put in it, and nothing re-scopes one.
type ApiKey struct {
	Id            string   `json:"clientId,omitzero"`
	Secret        string   `json:"clientSecret,omitzero"`
	SecretCommand []string `json:"clientSecretCommand,omitzero"`

	AccessToken IssuedToken `json:"accessToken,omitzero"`
}

// Manual is a token somebody pasted in. The token is the whole of it, so an expired one leaves
// nothing to mint from.
type Manual struct {
	AccessToken IssuedToken `json:"accessToken,omitzero"`
}

type IssuedToken struct {
	Token     jwt.JWT   `json:"token,omitzero"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// FromLogin, FromApiKey and FromManual set Current and its pointer together, so that no caller
// has to remember both halves of "presence is not selection".
func FromLogin(l Login) Credential { return Credential{Current: MethodLogin, Login: &l} }

func FromApiKey(k ApiKey) Credential { return Credential{Current: MethodApiKey, ApiKey: &k} }

func FromManual(m Manual) Credential { return Credential{Current: MethodManual, Manual: &m} }

// Validate is for a file somebody edited by hand — the constructors cannot produce a credential
// that names a method it does not hold. Without it, `current: apiKey` with no `apiKey:` block
// resolves to a credential nobody selected and fails much later.
//
// The zero value is valid: it is the profile nothing has logged in to yet.
func (c Credential) Validate() error {
	if c.Current == "" {
		var held []string
		if c.Login != nil {
			held = append(held, string(MethodLogin))
		}
		if c.ApiKey != nil {
			held = append(held, string(MethodApiKey))
		}
		if c.Manual != nil {
			held = append(held, string(MethodManual))
		}
		if held == nil {
			return nil
		}
		return diags.Errorf("This credential selects no authentication method",
			"It holds %s but names none as current. Set `current` to one of them, or log in again.",
			strings.Join(held, " and "))
	}
	switch c.Current {
	case MethodLogin:
		if c.Login != nil {
			return nil
		}
	case MethodApiKey:
		if c.ApiKey != nil {
			return nil
		}
	case MethodManual:
		if c.Manual != nil {
			return nil
		}
	default:
		return diags.Errorf("Unknown authentication method",
			"The credential records %q, which this version of the meshStack CLI does not know.", c.Current)
	}
	return diags.Errorf("This credential selects a method it does not hold",
		"It names %q as current but carries no %q entry. Log in again to store one.", c.Current, c.Current)
}
