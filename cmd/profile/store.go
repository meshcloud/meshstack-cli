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

func remove(ctx context.Context, profiles *profile.Profiles, name profile.Name) error {
	currentChanged, err := profiles.Remove(ctx, name)
	if err != nil || !currentChanged {
		return err
	}
	switch {
	case profiles.CurrentProfile != "":
		slog.WarnContext(ctx, fmt.Sprintf("Profile '%s' is the current one now, as it is the only one left.", profiles.CurrentProfile))
	case len(profiles.Profiles) > 0:
		slog.WarnContext(ctx, "No profile is current now. Make one the current one in meshstack profile, "+
			"or log in to it with meshstack login --profile <name>.")
	}
	return nil
}
