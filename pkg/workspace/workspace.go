// Package workspace names the meshStack workspace a command acts in.
//
// The workspace has two jobs, and only the first one depends on how the caller
// authenticated:
//
//  1. It is the token scope, for the browser login method only. meshfed builds a
//     user's rights from the MC_CUSTOMER claim, which a keycloak script mapper writes
//     from the c:<workspace> scope on the token request, so a user access token is
//     bound to exactly one workspace and the way to change it is another refresh
//     grant. An API key token carries whatever workspace its issuer put in it, and
//     nothing re-scopes one.
//
//  2. It is a request parameter, for every method. On the meshObject API a
//     workspace-bound principal's workspaceIdentifier parameter is ignored, while a
//     principal holding the matching ADM_ authority is not workspace-bound and the
//     parameter decides what it reads.
//
// So a workspace is always meaningful, even where it cannot change the token. It is a
// default rather than a boundary: one credential can reach objects owned by other
// workspaces, so the resolved workspace decides what a command acts in unless the
// command says otherwise, and nothing more.
package workspace

import (
	"errors"
	"os"
	"strings"
)

// Name identifies a workspace. meshStack calls it the workspace identifier.
type Name string

func (n Name) String() string { return string(n) }

// Scope keys one cached access token in a profile's credentials file. It is either
// Unscoped or "w:<workspace>"; the prefix is what keeps a workspace whose identifier
// happens to be "unscoped" from colliding with the unscoped entry.
//
// The wire scope stays c:<workspace> because that is what meshfed's MC_CUSTOMER script
// mapper reads — c for customer, from before workspaces were called workspaces.
type Scope string

const Unscoped Scope = "unscoped"

// Scope returns the cache key for a token carrying this workspace. The empty name is
// the unscoped token.
func (n Name) Scope() Scope {
	if n == "" {
		return Unscoped
	}
	return Scope("w:" + n)
}

// WireScope returns the value of the OAuth scope parameter that mints a token for this
// workspace. The empty name asks for an unscoped token.
func (n Name) WireScope() string {
	if n == "" {
		return "openid"
	}
	return "openid c:" + string(n)
}

// envKey is private, so that no front end assembles a sentence out of a constant it
// imported. Every message that has to name the variable is produced here.
const envKey = "MESHSTACK_WORKSPACE"

// FromEnv reads the workspace from the environment, or returns the empty name.
func FromEnv() Name {
	return Name(strings.TrimSpace(os.Getenv(envKey)))
}

// ErrMissing is what a command returns when it needs a workspace and none resolved.
// A browser login is the case that needs one: an unscoped user token reaches almost
// nothing, so failing here is better than surfacing meshfed's message, which names
// neither the flag nor the profile setting.
var ErrMissing = errors.New(`this profile has no workspace, and a browser login needs one.
Set it with --workspace, ` + envKey + `, or ` + "`meshstack profile set workspace <name>`" + `.
` + "`meshstack workspace list`" + ` shows the ones you can use`)
