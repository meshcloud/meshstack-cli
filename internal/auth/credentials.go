package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func (s Session) resolveCredentials(ctx context.Context, currentProfile *profile.Profile, opts ResolveSessionOptions) (profile.Credentials, credential.Credential, error) {
	creds, err := currentProfile.Credentials(ctx)
	if err != nil {
		return creds, nil, err
	}

	var resolvedCredentials []credential.Credential
	setResolvedCredential := func(resolved credential.Credential, err error) error {
		if err == nil && resolved != nil {
			creds.SetIdentity(resolved)
			resolvedCredentials = append(resolvedCredentials, resolved)
		}
		return err
	}

	if err := setResolvedCredential(s.resolveManualCredential(ctx, opts)); err != nil {
		return creds, nil, err
	}
	if err := setResolvedCredential(s.resolveApiKeyCredential(ctx, opts)); err != nil {
		return creds, nil, err
	}

	switch len(resolvedCredentials) {
	case 0:
		if currentProfile.Credential == "" {
			return creds, nil, fmt.Errorf("profile '%s' selects no credential; run 'meshstack login'", currentProfile)
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
