package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

// withLockedProfiles holds the profiles it loads until fn returns, so that a login does not store
// over what fn changes.
func withLockedProfiles(ctx context.Context, fn func(profile.Profiles) error) (err error) {
	profiles, err := profile.LoadProfiles(ctx, profile.LoadProfilesOptions{SettingSources: internal.SettingSources(), ExclusiveLock: true})
	if errors.Is(err, profile.ErrInUse) {
		// Only the cause, as the setting a lookup failed for adds nothing to do about it.
		return fmt.Errorf("%w, such as a login or another meshstack profile; let it finish and try again", profile.ErrInUse)
	}
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, profiles.Unlock())
	}()
	return fn(profiles)
}

func find(profiles profile.Profiles, name profile.Name) (*profile.Profile, error) {
	found, ok := profiles.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("there is no profile '%s'", name)
	}
	return found, nil
}

// put stores edited in place of original, or adds it where original is nil.
func put(ctx context.Context, profiles *profile.Profiles, original *profile.Profile, edited profile.Profile) error {
	if _, taken := profiles.Profiles[edited.Name]; taken && (original == nil || original.Name != edited.Name) {
		return fmt.Errorf("a profile named '%s' exists already", edited.Name)
	}
	if original != nil {
		if err := moveCredentials(ctx, *original, edited); err != nil {
			return err
		}
		if !original.Endpoint.Equal(edited.Endpoint) {
			edited.Credential = ""
		}
		delete(profiles.Profiles, original.Name)
		if profiles.CurrentProfile == original.Name {
			profiles.CurrentProfile = edited.Name
		}
	}
	profiles.Add(edited)
	if profiles.CurrentProfile == "" {
		profiles.CurrentProfile = edited.Name
	}
	return profiles.Store(ctx)
}

func moveCredentials(ctx context.Context, original, edited profile.Profile) error {
	if !original.Endpoint.Equal(edited.Endpoint) {
		return original.RemoveCredentials(ctx)
	}
	if original.Name == edited.Name {
		return nil
	}
	credentials, err := original.Credentials(ctx)
	if err != nil {
		return err
	}
	var none profile.Credentials
	if credentials.Credentials != none.Credentials {
		credentials.FilePath = original.ConfigDir.CredentialsJsonFor(edited.Name)
		if err := credentials.Store(ctx); err != nil {
			return err
		}
	}
	// The token caches go as well, and new tokens are minted on next use.
	return original.RemoveCredentials(ctx)
}

func remove(ctx context.Context, profiles *profile.Profiles, name profile.Name) error {
	removed, err := find(*profiles, name)
	if err != nil {
		return err
	}
	if err := removed.RemoveCredentials(ctx); err != nil {
		return err
	}
	delete(profiles.Profiles, name)
	if profiles.CurrentProfile != name {
		return profiles.Store(ctx)
	}
	// Of several profiles left, none is picked: the current one is what a command uses without
	// asking, so the user picks it.
	left := profiles.Selection().Profiles
	profiles.CurrentProfile = ""
	if len(left) == 1 {
		profiles.CurrentProfile = left[0].Name
	}
	if err := profiles.Store(ctx); err != nil {
		return err
	}
	switch {
	case len(left) == 1:
		slog.WarnContext(ctx, fmt.Sprintf("Profile '%s' is the current one now, as it is the only one left.", left[0].Name))
	case len(left) > 1:
		slog.WarnContext(ctx, "No profile is current now. Make one the current one in meshstack profile, "+
			"or log in to it with meshstack login --profile <name>.")
	}
	return nil
}

func use(ctx context.Context, profiles *profile.Profiles, name profile.Name) error {
	if _, err := find(*profiles, name); err != nil {
		return err
	}
	profiles.CurrentProfile = name
	return profiles.Store(ctx)
}
