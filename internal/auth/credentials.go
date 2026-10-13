package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func (s Session) resolveCredentials(ctx context.Context, opts ResolveSessionOptions) (Credential, error) {
	var resolved []Credential
	var errs, noSourceErrs []error
	collect := func(cred Credential, err error) {
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
	collect(s.resolveApiKeyCredential(ctx, opts.SettingSources))
	collect(s.resolveBasicAuthCredential(ctx, opts.SettingSources))
	collect(s.resolveManualCredential(ctx, opts.SettingSources))
	if err := errors.Join(errs...); err != nil {
		return Credential{}, err
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
		return Credential{}, fmt.Errorf("resolved more than one credential %v; please check environment MESHSTACK_* and/or explicit config", names)
	}
}

func (s Session) storedCredential(ctx context.Context, noSourceErrs []error) (Credential, error) {
	currentProfile := s.CurrentProfile
	if currentProfile.Credential == "" {
		return Credential{}, errors.Join(append([]error{
			fmt.Errorf("profile '%s' selects no credential; run 'meshstack login -p %s'", currentProfile, currentProfile),
		}, noSourceErrs...)...)
	}
	creds, err := currentProfile.Credentials(ctx)
	if err != nil {
		return Credential{}, err
	}
	current := creds.ByName(currentProfile.Credential)
	if current == nil {
		return Credential{}, fmt.Errorf("profile '%s' selects credential '%s', but %s holds none; run 'meshstack login -p %s'",
			currentProfile, currentProfile.Credential, creds.FilePath, currentProfile)
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using credential %s of profile %s", currentProfile.Credential, currentProfile))
	stored := CacheFor(currentProfile, current)
	stored.Sources, stored.Stored = []string{"file " + creds.FilePath}, true
	return stored, nil
}

// resolveBoth resolves two settings that make up one credential, and fails where only one is set.
func resolveBoth[A, B any](ctx context.Context, settingSources setting.Sources, credentialLabel string, a setting.Setting[A], b setting.Setting[B]) (valueA A, valueB B, sources []string, err error) {
	valueA, sourceA, errA := settingSources.ResolveSettingWithSource(ctx, a)
	valueB, sourceB, errB := settingSources.ResolveSettingWithSource(ctx, b)
	missingA := errors.Is(errA, setting.ErrNoSourceProvidedValue)
	missingB := errors.Is(errB, setting.ErrNoSourceProvidedValue)
	switch {
	case missingA && missingB:
		return valueA, valueB, nil, errors.Join(errA, errB)
	case missingA || missingB:
		// Flattened to text, since the caller skips a credential whose error wraps ErrNoSourceProvidedValue.
		return valueA, valueB, nil, errors.New(credentialLabel + " needs " + a.EnvKey() + " and " + b.EnvKey() +
			" together: " + errors.Join(errA, errB).Error())
	}
	if err := errors.Join(errA, errB); err != nil {
		return valueA, valueB, nil, err
	}
	return valueA, valueB, []string{sourceA.Describe(a.EnvKey()), sourceB.Describe(b.EnvKey())}, nil
}
