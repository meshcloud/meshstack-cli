package meshstack

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	oidc "github.com/meshcloud/meshstack-cli/internal/oidc/scope"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

// Workspace identifies a workspace as meshPanel shows it.
type Workspace string

// NoWorkspace is a session acting in no workspace at all.
const NoWorkspace Workspace = ""

// AsScope is the scope that binds a token to this workspace.
func (w Workspace) AsScope() oidc.Scope {
	const (
		scopePrefix = "c:"
		// unscoped is used as key in the OidcLogin credentials cache,
		// so its value can't be easily changed (keep it const)
		unscoped oidc.Scope = "unscoped"
	)
	if w == NoWorkspace {
		return unscoped
	}
	return oidc.Scope(scopePrefix + string(w))
}

func WorkspaceFromToken(token jwt.JWT) Workspace {
	return Workspace(token.GetClaim(jwt.WorkspaceClaim))
}

var WorkspaceSetting = setting.Setting[Workspace]{
	Env: "MESHSTACK_WORKSPACE",
	Short: func(envKey string) string {
		return fmt.Sprintf("The workspace to act in, identified as in meshPanel, such as my-workspace-ab12c. Also read from %s.", envKey)
	},
	Parse: setting.ParseText[Workspace],
}
