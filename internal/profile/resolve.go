package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type (
	SettingSources        = setting.Sources
	ResolveProfileOptions struct {
		SettingSources
	}
)

// ResolveProfile loads every profile from disk, creating a default one when there is none. The
// returned *Profile points into Profiles.Profiles, so a change through the pointer is persisted by
// a later Profiles.Store.
func ResolveProfile(ctx context.Context, opts ResolveProfileOptions) (*Profile, Profiles, error) {
	loadProfiles := sync.OnceValues(func() (Profiles, error) {
		return LoadProfiles(ctx, opts)
	})

	// Both are fallback sources, so a name given in MESHSTACK_PROFILE wins over what is on disk,
	// as NameSetting's own help text says it does. The endpoint match comes first, so that a run
	// against a known endpoint picks its profile rather than the one last selected.
	endpointMatchingSource := setting.FallbackSource{Source: setting.LookupSource{
		Description: "unique match by endpoint",
		Func: func(ctx context.Context) (string, error) {
			profiles, err := loadProfiles()
			if err != nil {
				return "", err
			}
			return profiles.findProfileNameByMatchingEndpoint(ctx, opts)
		},
	}}

	currentProfileSource := setting.FallbackSource{Source: setting.LookupSource{
		Description: "current profile",
		Func: func(_ context.Context) (string, error) {
			profiles, err := loadProfiles()
			return string(profiles.CurrentProfile), err
		},
	}}

	name, err := opts.ResolveSetting(ctx, NameSetting,
		endpointMatchingSource,
		currentProfileSource,
	)
	if err != nil {
		return nil, Profiles{}, err
	}

	if profiles, err := loadProfiles(); err != nil {
		return nil, profiles, err
	} else if profile, ok := profiles.Profiles[name]; !ok {
		return nil, profiles, fmt.Errorf("no profile found with name %s", name)
	} else {
		return profile, profiles, nil
	}
}

func (ps Profiles) findProfileNameByMatchingEndpoint(ctx context.Context, opts ResolveProfileOptions) (string, error) {
	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting)
	if errors.Is(err, setting.ErrNoSourceProvidedValue) {
		slog.DebugContext(ctx, "No endpoint known at this point, cannot search for matching profile")
		return "", nil
	} else if err != nil {
		return "", err
	}
	var matchingProfiles []*Profile
	for _, profile := range ps.Profiles {
		if profile.Endpoint != nil && profile.Endpoint.Equal(endpoint) {
			matchingProfiles = append(matchingProfiles, profile)
		}
	}
	if len(matchingProfiles) == 1 {
		profile := matchingProfiles[0]
		slog.DebugContext(ctx, fmt.Sprintf("Using profile %s by uniquely matching endpoint '%s'", profile, endpoint))
		return string(profile.Name), nil
	}
	return "", nil
}
