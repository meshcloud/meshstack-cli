package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ http.Authorization = Session{}

func (s Session) GetBearerToken(ctx context.Context) (out http.BearerToken, err error) {
	return s.RefreshBearerToken(ctx, "")
}

func (s Session) RefreshBearerToken(ctx context.Context, rejected http.BearerToken) (out http.BearerToken, err error) {
	// Resolved before any cache lock is taken: resolving the workspace may list workspaces, and
	// that request needs a token of its own, which takes the same lock. A failure is returned by
	// the calls below, which get the memoized result.
	_, _ = s.getWorkspace()
	usable := func() bool {
		token, found := s.Credential.CachedToken(ctx, s.getWorkspace)
		if !found || token.GetClaim(jwt.ExpiryClaim).Expired(30*time.Second) || token.String() == string(rejected) {
			return false
		}
		out = http.BearerToken(token.String())
		return true
	}

	var foundCached bool
	err = s.Credential.Read(ctx, func() error {
		foundCached = usable()
		return nil
	})
	if err != nil || foundCached {
		return
	}
	err = s.Credential.Modify(ctx, func() error {
		if usable() {
			return nil
		}
		// Not cancelled with ctx, so that Ctrl+C or a short deadline cannot cut a refresh off after the
		// issuer has rotated the refresh token: the rotated one would be lost, and with it the login.
		// Modify writes it to the cache before it returns, and the HTTP client's timeouts still end it.
		if refreshErr := s.Credential.RefreshCachedToken(context.WithoutCancel(ctx), s.httpClient, s.getWorkspace); refreshErr != nil {
			return refreshErr
		}
		// RefreshCachedToken guarantees a cached token, so found is always true here.
		token, _ := s.Credential.CachedToken(ctx, s.getWorkspace)
		out = http.BearerToken(token.String())
		return nil
	})
	if noRole, ok := errors.AsType[meshstack.NoRoleInWorkspaceError](err); ok {
		noRole.Existence = s.existenceOf(ctx, noRole.Workspace)
		err = noRole
	}
	return
}

// existenceOf reads the workspace with a token for the profile's default workspace, which lets an
// Organization Admin read every workspace, and anyone else learn of one that does not exist. A token
// for no workspace only tells the latter.
func (s Session) existenceOf(ctx context.Context, workspace meshstack.Workspace) meshstack.Existence {
	readFrom := s.CurrentProfile.DefaultWorkspace
	if readFrom == workspace {
		readFrom = meshstack.NoWorkspace
	}
	reading := s
	reading.getWorkspace = func() (meshstack.Workspace, error) {
		return readFrom, nil
	}
	read, err := client.New(ctx, s.CurrentProfile.Endpoint, s.httpClient, reading).Workspace.Read(ctx, string(workspace))
	switch {
	case err != nil:
		slog.DebugContext(ctx, fmt.Sprintf("Cannot tell whether workspace %s exists: %s", workspace, err.Error()))
		return meshstack.ExistenceUnknown
	case read == nil:
		return meshstack.DoesNotExist
	default:
		return meshstack.Exists
	}
}
