package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// newWorkspaceSelectionSource lets the person pick one of the workspaces this login can reach. It
// is a fallback source, so --workspace and MESHSTACK_WORKSPACE are taken as given, while the
// profile's default ranks below it: a login is what changes that default. It needs no --stdin,
// unlike the secret prompts in stdin.go, because the list has to be shown for the choice to make sense.
func newWorkspaceSelectionSource(p prompt.Prompt) setting.FallbackSource {
	return setting.FallbackLookupSource(setting.Workspace.EnvKey(), "the workspace selection of this login",
		func(ctx context.Context) (string, error) {
			workspaces, err := setting.WorkspacesFromContext(ctx)
			if err != nil {
				return "", err
			}
			candidates := make([]setting.MeshWorkspace, 0, len(workspaces.Items))
			for _, workspace := range workspaces.All() {
				candidates = append(candidates, workspace)
			}
			profileDefault := workspaces.ProfileDefault(ctx)
			selected, err := prompt.Select(ctx, p, "workspace", candidates, func(workspace setting.MeshWorkspace) bool {
				return profileDefault != nil && workspace.Matches(*profileDefault)
			})
			if err != nil {
				return "", err
			}
			return string(selected.Name()), nil
		})
}
