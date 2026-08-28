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

	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

// Name identifies a workspace. meshStack calls it the workspace identifier.
type Name string

func (n Name) String() string { return string(n) }

func (n Name) Empty() bool { return strings.TrimSpace(string(n)) == "" }

const (
	// Unscoped keys the token of a session that acts in no workspace. It is not a scope any
	// grant asks for: a request that names no workspace sends no c: scope at all.
	Unscoped scope.Scope = "unscoped"
	// ClaimKey for workspace reads unexpected, but in the early meshstack days, we had multiple customers instead of workspaces.
	ClaimKey = "MC_CUSTOMER"

	envKey = "MESHSTACK_WORKSPACE"
	// scopePrefix also refers to (obsolete) ClaimKey value, 'c' meaning customer.
	scopePrefix = "c:"
)

// Scope returns the cache key for a token carrying this workspace. An empty name is
// the unscoped token.
func (n Name) Scope() scope.Scope {
	if n.Empty() {
		return Unscoped
	}
	return scope.Scope(scopePrefix + string(n))
}

// envKey is private, so that no front end assembles a sentence out of a constant it
// imported. Every message that has to name the variable is produced here.

// FromEnv reads the workspace from the environment, or returns an empty name.
func FromEnv() Name {
	return Name(strings.TrimSpace(os.Getenv(envKey)))
}

// ErrMissing is what a command returns when it needs a workspace and none resolved.
// A browser login is the case that needs one: an unscoped user token reaches almost
// nothing, so failing here is better than surfacing meshfed's message, which names
// neither the flag nor the profile setting.
var ErrMissing = errors.New(`a browser login is bound to one workspace, and none is configured.
Name one with ` + envKey + `, with the meshStack CLI's --workspace flag or the Terraform
provider's workspace attribute, or make it the profile's default with
` + "`meshstack profile set workspace <name>`" + `.
` + "`meshstack workspace list`" + ` shows the ones you can use`)
