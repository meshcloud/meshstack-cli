package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type credentialResolver struct {
	// stored is read for its type alone, which names the field and thus the ForceAuthWith value
	stored  credential.Credential
	resolve func(context.Context, ResolveSessionOptions) (credential.Credential, error)
}

// credentialResolvers is what may mint a credential, in the order they are tried. A resolver that
// needs a person — the browser login — is in the list only when forced names it, so that an
// unforced resolution cannot reach one: the Terraform provider resolves a session on every plan
// and must never open a browser.
func (s Session) credentialResolvers(forced credential.Name) []credentialResolver {
	resolvers := []credentialResolver{
		{new(credential.Manual), s.resolveManualCredential},
		{new(credential.ApiKey), s.resolveApiKeyCredential},
	}
	if forced == "" {
		return resolvers
	}
	return append(resolvers, credentialResolver{new(credential.OidcLogin), s.resolveOidcLoginCredential})
}

func (s Session) resolveCredentials(ctx context.Context, currentProfile *profile.Profile, opts ResolveSessionOptions) (profile.Credentials, credential.Credential, error) {
	creds, err := currentProfile.Credentials(ctx)
	if err != nil {
		return profile.Credentials{}, nil, err
	}

	var errs, noSourceErrs []error
	var resolvedCredentials []credential.Credential
	setResolvedCredential := func(resolved credential.Credential, err error) {
		if opts.ForceAuthWith == "" && errors.Is(err, setting.ErrNoSourceProvidedValue) {
			noSourceErrs = append(noSourceErrs, err)
			return
		} else if err != nil {
			errs = append(errs, err)
			return
		}
		creds.SetIdentity(resolved)
		resolvedCredentials = append(resolvedCredentials, resolved)
	}

	if opts.ForceAuthWith != "" && !slices.Contains(credential.Names, opts.ForceAuthWith) {
		return profile.Credentials{}, nil, fmt.Errorf("cannot authenticate with credential '%s'; pick one of %v", opts.ForceAuthWith, credential.Names)
	}
	for _, resolver := range s.credentialResolvers(opts.ForceAuthWith) {
		if opts.ForceAuthWith != "" && opts.ForceAuthWith != creds.NameOf(resolver.stored) {
			continue
		}
		setResolvedCredential(resolver.resolve(ctx, opts))
	}
	if err := errors.Join(errs...); err != nil {
		return profile.Credentials{}, nil, err
	}

	switch len(resolvedCredentials) {
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
		currentProfile.Credential = creds.NameOf(resolvedCredentials[0])
		slog.DebugContext(ctx, fmt.Sprintf("Using uniquely resolved credential %s from environment MESHSTACK_* and/or explicit config", currentProfile.Credential))
		return creds, resolvedCredentials[0], nil
	default:
		resolvedNames := make([]credential.Name, 0, len(resolvedCredentials))
		for _, resolved := range resolvedCredentials {
			resolvedNames = append(resolvedNames, creds.NameOf(resolved))
		}
		return creds, nil, fmt.Errorf("resolved more than one credential %v; please check environment MESHSTACK_* and/or explicit config", resolvedNames)
	}
}
