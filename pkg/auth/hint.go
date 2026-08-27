package auth

import (
	"errors"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/auth/method"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
)

// HintErr logs a warning for the two failures whose cause is not in the error text, and
// returns err untouched. It takes the Authorization because a hint names the scope the token
// carried.
//
// It never retries and never re-scopes: a command acts in the workspace it was told to act
// in. meshfed does answer an insufficiently scoped request with an insufficient_scope
// challenge, but only on the internal API's workspace switch — the public API answers a plain
// forbidden with no scope hint, so there is nothing for the CLI to read even if it wanted to.
func HintErr(err error, authz client.Authorization) error {
	if err == nil {
		return nil
	}
	session, _ := authz.(*Session)

	var httpErr client.HttpError
	switch {
	case errors.As(err, &httpErr) && httpErr.IsForbidden():
		slog.Warn(forbiddenHint(session))
	case errors.Is(err, oidc.ErrRefreshRejected):
		slog.Warn("this login has expired or was revoked. Run `meshstack login`.")
	}
	return err
}

func forbiddenHint(session *Session) string {
	if session == nil {
		return "the credential this command used does not reach that object."
	}
	// A token that carries no workspace scope at all — an API key or a pasted token — gets the
	// same note without a workspace name, because the credential's own workspace is what
	// decides what it reaches.
	if session.Method() != method.Login || session.Workspace == "" {
		return "this credential's own workspace is what decides what it reaches; a --workspace on the command line cannot widen it."
	}
	return "this token is scoped to workspace " + session.Workspace.String() +
		". If the object belongs to another workspace, name it with --workspace."
}
