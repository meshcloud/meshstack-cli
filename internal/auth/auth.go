package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ http.Authorization = Session{}

func (s Session) GetCredential(ctx context.Context) (http.Credential, error) {
	return s.authorization().GetCredential(ctx)
}

func (s Session) RefreshCredential(ctx context.Context, rejected http.Credential) (http.Credential, error) {
	return s.authorization().RefreshCredential(ctx, rejected)
}

func (s Session) authorization() http.Authorization {
	switch cred := s.Credential.Credential.(type) {
	case credential.Static:
		return cred.Authorization()
	case credential.Minter:
		return mintedToken{s, cred}
	default:
		panic(fmt.Sprintf("credential %T neither mints a token nor is static", cred))
	}
}

// CheckCredential fails where meshStack refuses the credential.
func (s Session) CheckCredential(ctx context.Context) error {
	_, err := s.httpClient.WithAuthorization(s).DoRequest[[]byte](ctx, http.MethodGet, s.CurrentProfile.Endpoint.JoinPath("api", "meshobjects", "meshworkspaces"))
	if httpErr, ok := errors.AsType[http.Error](err); ok && !httpErr.IsUnauthorized() {
		// meshStack authenticates before it authorizes, so any other refusal accepted the credential.
		return nil
	}
	return err
}

func (s Session) GetBearerToken(ctx context.Context) (http.BearerToken, error) {
	cred, err := s.GetCredential(ctx)
	if err != nil {
		return "", err
	}
	token, ok := cred.(http.BearerToken)
	if !ok {
		return "", fmt.Errorf("%s sends no bearer token", s.Credential.Name().Label())
	}
	return token, nil
}

// mintedToken is the Authorization of a credential that mints, caches and renews a bearer token.
type mintedToken struct {
	session Session
	minter  credential.Minter
}

func (t mintedToken) GetCredential(ctx context.Context) (http.Credential, error) {
	return t.bearerToken(ctx, nil)
}

func (t mintedToken) RefreshCredential(ctx context.Context, rejected http.Credential) (http.Credential, error) {
	return t.bearerToken(ctx, rejected)
}

func (t mintedToken) Scope() string {
	return t.session.Scope()
}

func (t mintedToken) bearerToken(ctx context.Context, rejected http.Credential) (out http.BearerToken, err error) {
	s, minter := t.session, t.minter
	// Resolved before any cache lock is taken: resolving the workspace may list workspaces, and
	// that request needs a token of its own, which takes the same lock. A failure is returned by
	// the calls below, which get the memoized result.
	_, _ = s.getWorkspace()
	usable := func() bool {
		token, found := minter.CachedToken(ctx, s.getWorkspace)
		if !found || token.GetClaim(jwt.ExpiryClaim).Expired(30*time.Second) || http.BearerToken(token.String()) == rejected {
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
		if refreshErr := minter.RefreshCachedToken(context.WithoutCancel(ctx), s.httpClient, s.getWorkspace); refreshErr != nil {
			return refreshErr
		}
		// RefreshCachedToken guarantees a cached token, so found is always true here.
		token, _ := minter.CachedToken(ctx, s.getWorkspace)
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
