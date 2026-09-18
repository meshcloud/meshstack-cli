package profile

import (
	"context"

	"github.com/meshcloud/meshstack-cli/internal/profile"
)

type (
	// Name identifies a profile, and is what MESHSTACK_PROFILE carries.
	Name = profile.Name
	// Profile is a named bundle of endpoint, default workspace and credential.
	Profile = profile.Profile
	// ResolveProfileOptions carries the setting sources the resolution reads.
	ResolveProfileOptions = profile.ResolveProfileOptions
)

// ResolveProfile returns the current profile, creating a default one when none exists.
func ResolveProfile(ctx context.Context, opts ResolveProfileOptions) (Profile, error) {
	currentProfile, _, err := profile.ResolveProfile(ctx, opts)
	if err != nil {
		return Profile{}, err
	}
	return *currentProfile, err
}
