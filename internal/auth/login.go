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

// Login holds the profiles it stores until unlock, so that no other command that stores them, such
// as meshstack profile, stores over the profile of a login waiting for the browser. Where Login
// fails, it holds nothing. An unlock before the credential is stored also takes back the tokens
// that the login cached.
func Login(ctx context.Context, withAuth credential.Name, opts ResolveSessionOptions) (_ Session, _ StoreFunc, unlock func() error, err error) {
	session, profiles, err := newSession(ctx, opts, true)
	if err != nil {
		return Session{}, nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, profiles.Unlock())
		}
	}()

	var creds profile.Credentials
	creds, err = session.CurrentProfile.Credentials(ctx)
	if err != nil {
		return Session{}, nil, nil, err
	}

	var resolved Credential
	switch withAuth {
	case credential.OidcLoginName:
		resolved, err = session.resolveOidcLoginCredential(ctx, creds.OidcLogin)
	case credential.ManualName:
		resolved, err = session.resolveManualCredential(ctx, opts.SettingSources)
	case credential.ApiKeyName:
		resolved, err = session.resolveApiKeyCredential(ctx, opts.SettingSources)
	default:
		return Session{}, nil, nil, fmt.Errorf("cannot authenticate with credential '%s'; pick one of %v", withAuth, credential.Names)
	}
	if err != nil {
		return Session{}, nil, nil, err
	}
	creds.Set(resolved.Credential)

	slog.DebugContext(ctx, fmt.Sprintf("Setting credential %s in profile", withAuth))
	session.CurrentProfile.Credential = withAuth

	resolved.Sources, resolved.Stored = []string{"file " + creds.FilePath}, true
	session.Credential = resolved
	restoreCache, err := session.Credential.WriteOver(ctx)
	if err != nil {
		return Session{}, nil, nil, err
	}

	session.Client = sync.OnceValues(func() (client.Client, error) {
		return session.buildClient(ctx, opts)
	})

	session.getWorkspace = sync.OnceValues(func() (meshstack.Workspace, error) {
		return session.resolveWorkspaceForLogin(ctx, opts)
	})

	var credentialStored bool
	storeSession := func(ctx context.Context) error {
		// A login that fails stores nothing, so it neither switches the current profile nor leaves a
		// credential behind that a later command would use.
		switch defaultWorkspace, err := session.getWorkspace(); {
		case err == nil:
			session.CurrentProfile.DefaultWorkspace = defaultWorkspace
		case !errors.Is(err, setting.ErrNoSourceProvidedValue):
			return err
		}

		// CurrentProfile points into the map profiles holds, so the default workspace set above is
		// stored as well.
		storeErr := creds.Store(ctx)
		credentialStored = storeErr == nil
		return errors.Join(storeErr, profiles.SetCurrent(ctx, session.CurrentProfile.Name))
	}
	unlockAndRestore := func() error {
		var restoreErr error
		if !credentialStored {
			// Not cancelled with ctx, so that a login cut off with Ctrl+C still takes its tokens back.
			restoreErr = restoreCache(context.WithoutCancel(ctx))
		}
		return errors.Join(restoreErr, profiles.Unlock())
	}
	return session, storeSession, unlockAndRestore, nil
}

func (s Session) resolveWorkspaceForLogin(ctx context.Context, opts ResolveSessionOptions) (meshstack.Workspace, error) {
	// A source that resolves the workspace usually wants the list to pick from, so the context
	// carries a lazy fetch of it. That fetch lists through a client naming no workspace: the
	// session's own client resolves the workspace first, which is the resolution running here.
	ctxWithWorkspaces := meshstack.SetWorkspacesInContext(ctx, sync.OnceValues(func() (r meshstack.Workspaces, err error) {
		r.ProfileDefaultWorkspace = s.CurrentProfile.DefaultWorkspace
		if meshInfo, infoErr := s.MeshInfo(); infoErr != nil {
			slog.WarnContext(ctx, "Cannot tell which workspace is the admin workspace: "+infoErr.Error())
		} else {
			r.AdminWorkspace = meshstack.Workspace(meshInfo.AdminWorkspaceIdentifier)
		}
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
			slog.DebugContext(ctx, "meshStack refused the workspace list: "+httpError.Error())
			err = meshstack.NoWorkspaceToWorkInError{MayNotList: true}
		} else if err == nil && len(r.Items) == 0 {
			err = meshstack.NoWorkspaceToWorkInError{}
		}
		return
	}))
	return opts.ResolveSetting(ctxWithWorkspaces, meshstack.WorkspaceSetting,
		s.CurrentProfile.WorkspaceSource(),
	)
}
