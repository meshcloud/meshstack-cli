package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func (s Session) resolveCredentials(ctx context.Context, opts ResolveSessionOptions) (credential.Credential, error) {
	var resolved []credential.Credential
	var errs, noSourceErrs []error
	collect := func(cred credential.Credential, err error) {
		switch {
		case errors.Is(err, setting.ErrNoSourceProvidedValue):
			noSourceErrs = append(noSourceErrs, err)
		case err != nil:
			errs = append(errs, err)
		default:
			resolved = append(resolved, cred)
		}
	}
	// No browser login here: it needs a person, so only Login reaches it, and the Terraform
	// provider resolves a session on every plan and must never open a browser.
	collect(s.resolveApiKeyCredential(ctx, opts))
	collect(s.resolveManualCredential(ctx, opts))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	switch len(resolved) {
	case 0:
		return s.storedCredential(ctx, noSourceErrs)
	case 1:
		slog.DebugContext(ctx, fmt.Sprintf("Using uniquely resolved credential %s from environment MESHSTACK_* and/or explicit config", resolved[0].Name()))
		return resolved[0], nil
	default:
		names := make([]credential.Name, 0, len(resolved))
		for _, cred := range resolved {
			names = append(names, cred.Name())
		}
		return nil, fmt.Errorf("resolved more than one credential %v; please check environment MESHSTACK_* and/or explicit config", names)
	}
}

func (s Session) storedCredential(ctx context.Context, noSourceErrs []error) (credential.Credential, error) {
	currentProfile := s.CurrentProfile
	if currentProfile.Credential == "" {
		return nil, errors.Join(append([]error{
			fmt.Errorf("no credential resolved, and profile '%s' selects none; run 'meshstack login'", currentProfile),
		}, noSourceErrs...)...)
	}
	creds, err := currentProfile.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	current := creds.ByName(currentProfile.Credential)
	if current == nil {
		return nil, fmt.Errorf("profile '%s' selects credential '%s', but %s holds none; run 'meshstack login'",
			currentProfile, currentProfile.Credential, creds.FilePath)
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using credential %s of profile %s", currentProfile.Credential, currentProfile))
	return current, nil
}
