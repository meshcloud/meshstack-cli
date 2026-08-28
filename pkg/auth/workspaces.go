package auth

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// Workspaces lists the workspaces this credential can see. `meshstack auth login` prompts
// from it, and `meshstack workspace list` prints it.
//
// It is the one thing an unscoped user token is good for: with no c: scope the principal
// holds only the three list rights, so this call works right after a browser login and
// before any workspace has been chosen.
func (s *Session) Workspaces(ctx context.Context) ([]workspace.Name, error) {
	// Deliberately unscoped. A user has to be able to list the workspaces *before* picking one,
	// and a scoped exchange for a workspace the user is not in fails with the very message this
	// list is meant to answer.
	api, err := s.unscoped().Client(ctx, userAgent)
	if err != nil {
		return nil, err
	}
	found, err := api.Workspace.List(ctx)
	if err != nil {
		return nil, HintErr(err, s)
	}
	names := make([]workspace.Name, 0, len(found))
	for _, w := range found {
		names = append(names, workspace.Name(w.Metadata.Name))
	}
	return names, nil
}

// Client builds the meshStack API client this session authenticates. Both front ends call it
// rather than client.New, so the endpoint and the authorization always agree with what was
// resolved. The user agent stays a parameter, because it is the one thing that genuinely
// differs: the Terraform provider identifies itself by its own name and version.
func (s *Session) Client(ctx context.Context, userAgent string) (client.Client, error) {
	return client.New(ctx, s.Endpoint.URL, userAgent, s)
}
