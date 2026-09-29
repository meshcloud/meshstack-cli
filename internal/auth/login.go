package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type StoreFunc func(context.Context) error

func Login(ctx context.Context, withAuth credential.Name, opts ResolveSessionOptions) (Session, StoreFunc, error) {
	session, storeProfiles, err := newSession(ctx, opts)
	if err != nil {
		return Session{}, nil, err
	}

	var creds profile.Credentials
	creds, err = session.CurrentProfile.Credentials(ctx)
	if err != nil {
		return Session{}, nil, err
	}

	var resolved credential.Credential
	switch withAuth {
	case credential.OidcLoginName:
		resolved, err = session.resolveOidcLoginCredential(ctx, creds.OidcLogin)
	case credential.ManualName:
		resolved, err = session.resolveManualCredential(ctx, opts.SettingSources)
	case credential.ApiKeyName:
		resolved, err = session.resolveApiKeyCredential(ctx, opts.SettingSources)
	default:
		return Session{}, nil, fmt.Errorf("cannot authenticate with credential '%s'; pick one of %v", withAuth, credential.Names)
	}
	if err != nil {
		return Session{}, nil, err
	}
	creds.Set(resolved)

	slog.DebugContext(ctx, fmt.Sprintf("Setting credential %s in profile", withAuth))
	session.CurrentProfile.Credential = withAuth

	session.Credential = session.CurrentProfile.CacheFor(resolved)
	if err := session.Credential.Write(ctx); err != nil {
		return Session{}, nil, err
	}

	session.Client = sync.OnceValues(func() (client.Client, error) {
		return session.buildClient(ctx, opts)
	})

	session.getWorkspace = sync.OnceValues(func() (meshstack.Workspace, error) {
		return session.resolveWorkspaceForLogin(ctx, opts)
	})

	storeSession := func(ctx context.Context) error {
		// The credentials go first, and every step runs even after an earlier one failed.
		errs := []error{creds.Store(ctx)}

		switch defaultWorkspace, err := session.getWorkspace(); {
		case err == nil:
			session.CurrentProfile.DefaultWorkspace = defaultWorkspace
		case !errors.Is(err, setting.ErrNoSourceProvidedValue):
			errs = append(errs, err)
		}

		// currentProfile points into the map profiles holds, so the assignment above is what
		// storeProfiles writes out.
		return errors.Join(append(errs, storeProfiles(ctx))...)
	}
	return session, storeSession, nil
}

func (s Session) resolveWorkspaceForLogin(ctx context.Context, opts ResolveSessionOptions) (meshstack.Workspace, error) {
	// A source that resolves the workspace usually wants the list to pick from, so the context
	// carries a lazy fetch of it. That fetch lists through a client naming no workspace: the
	// session's own client resolves the workspace first, which is the resolution running here.
	ctxWithWorkspaces := meshstack.SetWorkspacesInContext(ctx, sync.OnceValues(func() (r meshstack.Workspaces, err error) {
		r.ProfileDefaultWorkspace = s.CurrentProfile.DefaultWorkspace
		unscoped := s
		unscoped.getWorkspace = func() (meshstack.Workspace, error) {
			return meshstack.NoWorkspace, nil
		}
		var c client.Client
		c, err = unscoped.buildClient(ctx, opts)
		if err != nil {
			return r, err
		}
		r.Items, err = c.Workspace.List(ctx)
		if httpError, ok := errors.AsType[http.Error](err); ok && httpError.IsForbidden() {
			err = fmt.Errorf("cannot list workspaces; try logging into meshPanel UI first, got: %w", httpError)
		} else if err == nil && len(r.Items) == 0 {
			err = errors.New("no workspaces found; try logging into meshPanel UI first and/or become member of a workspace")
		}
		return
	}))
	return opts.ResolveSetting(ctxWithWorkspaces, meshstack.WorkspaceSetting,
		s.CurrentProfile.WorkspaceSource(),
	)
}
