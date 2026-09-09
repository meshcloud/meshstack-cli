// Package meshstack holds the names a command needs before it can act: the workspace it
// acts in, and the endpoint it acts against.
//
// The workspace does two jobs, and only the first depends on how the caller authenticated:
//
//  1. It is the token scope, for the browser login alone. meshfed builds a user's rights
//     from the MC_CUSTOMER claim, which a keycloak script mapper writes from the
//     c:<workspace> scope on the token request. So a user access token is bound to one
//     workspace, and another refresh grant is the only way to change it. Nothing re-scopes
//     an API key token.
//
//  2. It is a request parameter, for every method. The meshObject API ignores
//     workspaceIdentifier for a workspace-bound principal, and lets it decide what a
//     principal holding the matching ADM_ authority reads.
package meshstack

import (
	"errors"
	"os"
	"strings"

	"github.com/meshcloud/meshstack-cli/pkg/oidc/scope"
)

const (
	// Unscoped keys the token of a session that acts in no workspace. No grant asks for it:
	// a request that names no workspace sends no c: scope at all.
	Unscoped scope.Scope = "unscoped"

	// ClaimKey and scopePrefix both say customer, which is what meshStack called a workspace
	// before it was renamed.
	ClaimKey    = "MC_CUSTOMER"
	scopePrefix = "c:"

	// No MESHSTACK_* name is exported anywhere in this module, so every message that names
	// this one is written here rather than assembled by a front end out of a constant.
	envKey = "MESHSTACK_WORKSPACE"
)

func WorkspaceScope(name string) scope.Scope {
	if strings.TrimSpace(name) == "" {
		return Unscoped
	}
	return scope.Scope(scopePrefix + name)
}

func WorkspaceFromEnv() string {
	return strings.TrimSpace(os.Getenv(envKey))
}

var ErrMissing = errors.New(`a browser login is bound to one workspace, and none is configured.
Name one with ` + envKey + `, with the meshStack CLI's --workspace flag or the Terraform
provider's workspace attribute, or make it the profile's default with
` + "`meshstack profile set workspace <name>`" + `.
` + "`meshstack workspace list`" + ` shows the ones you can use`)
