package profile

import (
	"context"

	"github.com/meshcloud/meshstack-cli/internal/profile"
)

type (
	// Name identifies a profile, and is what MESHSTACK_PROFILE carries.
	Name = profile.Name
	// Profile describes access to a specific meshStack instance.
	Profile = profile.Profile
	// ResolveProfileOptions is used in ResolveProfile, mainly supplying additional setting sources.
	ResolveProfileOptions = profile.ResolveProfileOptions
)

// ResolveProfile returns the current profile loaded from config and settings, or inits a default one.
func ResolveProfile(ctx context.Context, opts ResolveProfileOptions) (Profile, error) {
	currentProfile, _, err := profile.ResolveProfile(ctx, opts)
	if err != nil {
		return Profile{}, err
	}
	return *currentProfile, err
}
