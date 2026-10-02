package meshstack

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

// Workspace identifies a workspace as meshPanel shows it.
type Workspace string

// NoWorkspace means that no workspace is known, or that the authentication is unscoped. It is the
// zero value, so `omitzero` leaves it out of a stored profile.
const NoWorkspace Workspace = ""

func (w Workspace) String() string {
	if w == NoWorkspace {
		return "<none>"
	}
	return string(w)
}

var WorkspaceSetting = setting.Setting[Workspace]{
	Env: "MESHSTACK_WORKSPACE",
	Short: func(envKey string) string {
		return fmt.Sprintf("The workspace to act in, identified as in meshPanel, such as my-workspace-ab12c. Also read from %s.", envKey)
	},
	Parse: setting.ParseText[Workspace],
}

// NoWorkspaceToWorkInError is what a login gets whose workspace list holds nothing to choose from.
type NoWorkspaceToWorkInError struct {
	// MayNotList is set where meshStack refused the list, rather than answering an empty one.
	MayNotList bool
}

func (e NoWorkspaceToWorkInError) Error() string {
	if e.MayNotList {
		return "this login has no workspace to work in, as it may not list workspaces; ask for access to one in meshPanel, or name one with --workspace"
	}
	return "this login has no workspace to work in; ask for access to one in meshPanel"
}

