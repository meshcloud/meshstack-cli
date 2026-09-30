package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// newWorkspaceSelectionSource is a fallback source, so --workspace and MESHSTACK_WORKSPACE are taken as given, while the
// profile's default ranks below it: a login is what changes that default. It needs no --stdin,
// unlike the secret prompts in stdin.go, because the list has to be shown for the choice to make sense.
//
// A login that works without a workspace passes optional, and then takes an input that ends before
// an answer as no selection. That is how a script with a closed stdin gets through, while a pipe that
// carries the answer still selects.
func newWorkspaceSelectionSource(p prompt.Prompt, optional bool) setting.FallbackSource {
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
			if optional && errors.Is(err, prompt.ErrEndOfInput) {
				slog.InfoContext(ctx, "No workspace was selected, as the input ended; the profile keeps its default workspace")
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return string(selected.Name()), nil
		})
}
