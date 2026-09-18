package meshstack

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

// Workspace identifies a workspace as meshPanel shows it.
type Workspace string

// NoWorkspace also nicely aligns with Profile.DefaultWorkspace with JSON omitzero.
// It is used to indicate that no workspace is known
// or being in an unscoped authentication.
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
