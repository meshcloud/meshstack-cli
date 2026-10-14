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

// NoRoleInWorkspaceError is what a login gets that asks for a token for a workspace in which it has
// no role. The issuer then mints a token for no workspace.
type NoRoleInWorkspaceError struct {
	Workspace Workspace
	Existence Existence
}

type Existence int

const (
	ExistenceUnknown Existence = iota
	Exists
	DoesNotExist
)

func (e NoRoleInWorkspaceError) Error() string {
	const roleNeeded = "A token for a workspace takes a role in it, also for an Organization Admin, whose login can still read the workspace without --workspace"
	switch e.Existence {
	case DoesNotExist:
		return fmt.Sprintf("workspace '%s' does not exist; check its identifier", e.Workspace)
	case Exists:
		return fmt.Sprintf("this login has no role in workspace '%s'. %s", e.Workspace, roleNeeded)
	default:
		return fmt.Sprintf("this login has no role in workspace '%s', or the workspace does not exist. %s", e.Workspace, roleNeeded)
	}
}
