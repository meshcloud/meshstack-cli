package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type credentialResolver func(context.Context, ResolveSessionOptions) (credential.Credential, error)

func (s Session) resolveCredentials(ctx context.Context, currentProfile *profile.Profile, opts ResolveSessionOptions) (profile.Credentials, credential.Credential, error) {
	creds, err := currentProfile.Credentials(ctx)
	if err != nil {
		return profile.Credentials{}, nil, err
	}

	resolvers := map[credential.Name]credentialResolver{
		credential.ApiKeyName:    s.resolveApiKeyCredential,
		credential.ManualName:    s.resolveManualCredential,
		credential.OidcLoginName: s.resolveOidcLoginCredential,
	}

	if forced := opts.ForceAuthWith; forced != "" {
		resolve, found := resolvers[forced]
		if !found {
			return profile.Credentials{}, nil, fmt.Errorf("cannot authenticate with credential '%s'; pick one of %v", forced, credential.Names)
		}
		resolved, err := resolve(ctx, opts)
		if err != nil {
			return profile.Credentials{}, nil, err
		}
		creds.SetIdentity(resolved)
		currentProfile.Credential = forced
		slog.DebugContext(ctx, fmt.Sprintf("Using credential %s, which was asked for by name", forced))
		return creds, resolved, nil
	}

	var errs, noSourceErrs []error
	var resolvedNames []credential.Name
	for _, name := range credential.Names {
		// A resolver that needs a person — the browser login — is reached only by the forced path
		// above: the Terraform provider resolves a session on every plan and must never open a browser.
		if name == credential.OidcLoginName {
			continue
		}
		switch resolved, err := resolvers[name](ctx, opts); {
		case errors.Is(err, setting.ErrNoSourceProvidedValue):
			noSourceErrs = append(noSourceErrs, err)
		case err != nil:
			errs = append(errs, err)
		default:
			creds.SetIdentity(resolved)
			resolvedNames = append(resolvedNames, name)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return profile.Credentials{}, nil, err
	}

	switch len(resolvedNames) {
	case 0:
		if currentProfile.Credential == "" {
			return creds, nil, errors.Join(append([]error{
				fmt.Errorf("no credential resolved, and profile '%s' selects none", currentProfile),
			}, noSourceErrs...)...)
		}
		current := creds.ByName(currentProfile.Credential)
		if current == nil {
			return creds, nil, fmt.Errorf("profile '%s' selects credential '%s', but %s holds none; run 'meshstack login'", currentProfile, currentProfile.Credential, creds.FilePath)
		}
		slog.DebugContext(ctx, fmt.Sprintf("Using credential %s of profile %s", currentProfile.Credential, currentProfile))
		return creds, current, nil
	case 1:
		currentProfile.Credential = resolvedNames[0]
		slog.DebugContext(ctx, fmt.Sprintf("Using uniquely resolved credential %s from environment MESHSTACK_* and/or explicit config", currentProfile.Credential))
		return creds, creds.ByName(resolvedNames[0]), nil
	default:
		return creds, nil, fmt.Errorf("resolved more than one credential %v; please check environment MESHSTACK_* and/or explicit config", resolvedNames)
	}
}
