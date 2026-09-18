package meshstack

import (
	"context"
	"fmt"
	"iter"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/client"
)

type (
	workspacesContextKey int
	WorkspacesFunc       func() (Workspaces, error)
)

func WorkspacesFromContext(ctx context.Context) (Workspaces, error) {
	workspaces, found := ctx.Value(workspacesContextKey(0)).(WorkspacesFunc)
	if !found {
		// An error rather than a panic, because a front end reaches this through pkg/setting and a
		// panic there takes its process down.
		return Workspaces{}, fmt.Errorf("no workspaces available in this context; they are only provided while resolving %s", WorkspaceSetting.EnvKey())
	}
	return workspaces()
}

func SetWorkspacesInContext(ctx context.Context, workspacesFunc WorkspacesFunc) context.Context {
	return context.WithValue(ctx, workspacesContextKey(0), workspacesFunc)
}

type (
	Workspaces struct {
		Items                   []client.MeshWorkspace
		ProfileDefaultWorkspace Workspace
	}
	MeshWorkspace struct {
		client.MeshWorkspace
	}
)

func (ws Workspaces) All() iter.Seq2[int, MeshWorkspace] {
	return func(yield func(int, MeshWorkspace) bool) {
		for i, item := range ws.Items {
			if !yield(i, MeshWorkspace{item}) {
				return
			}
		}
	}
}

func (ws Workspaces) ProfileDefault(ctx context.Context) (r *MeshWorkspace) {
	if ws.ProfileDefaultWorkspace == NoWorkspace {
		return
	}
	for _, item := range ws.All() {
		if item.Name() == ws.ProfileDefaultWorkspace {
			slog.DebugContext(ctx, fmt.Sprintf("Found profile's default workspace: %s", item))
			return &item
		}
	}
	slog.WarnContext(ctx, fmt.Sprintf("Your profile carries the default workspace '%s', which you have no access to currently", ws.ProfileDefaultWorkspace))
	return nil
}

func (ws Workspaces) Single(ctx context.Context) (single *MeshWorkspace) {
	if len(ws.Items) == 1 {
		single = &MeshWorkspace{ws.Items[0]}
		slog.InfoContext(ctx, fmt.Sprintf("Auto-selecting the only workspace available: %s", single))
	}
	return
}

func (w MeshWorkspace) Name() Workspace {
	return Workspace(w.Metadata.Name)
}

func (w MeshWorkspace) Matches(other MeshWorkspace) bool {
	return w.Name() == other.Name()
}

func (w MeshWorkspace) String() string {
	return fmt.Sprintf("%s (%s)", w.Spec.DisplayName, w.Metadata.Name)
}
