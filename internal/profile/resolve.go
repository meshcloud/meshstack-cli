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

type ResolveProfileOptions struct {
	setting.ExplicitSourcesOption
}

// ResolveProfile loads the current profile and loads all profiles from disk (if any).
// It ensures one (default) profile is present and will only fail if unmarshaling fails from disk.
func ResolveProfile(ctx context.Context, opts ResolveProfileOptions) (*Profile, Profiles, error) {
	loadProfiles := sync.OnceValues(func() (Profiles, error) {
		return LoadProfiles(ctx, opts)
	})

	endpointMatchingSource := setting.LookupSource{
		Description: "unique match by endpoint",
		Func: func() (string, error) {
			profiles, err := loadProfiles()
			if err != nil {
				return "", err
			}
			return profiles.findProfileNameByMatchingEndpoint(ctx, opts)
		},
	}

	currentProfileSource := setting.LookupSource{
		Description: "current profile",
		Func: func() (string, error) {
			profiles, err := loadProfiles()
			return string(profiles.CurrentProfile), err
		},
	}

	// Profile name always resolves (unless parsing error),
	// as NameSetting has a (static) 'default'.
	name, err := opts.ResolveSetting(NameSetting,
		// prefer profile matching an endpoint over the current profile stored on disk for convenience
		endpointMatchingSource,
		currentProfileSource,
	)
	if err != nil {
		return nil, Profiles{}, err
	}

	if profiles, err := loadProfiles(); err != nil {
		return nil, profiles, err
	} else if profile, ok := profiles.Profiles[name]; !ok {
		return profile, profiles, fmt.Errorf("no profile found with name %s", name)
	} else {
		return profile, profiles, nil
	}
}

func (ps Profiles) findProfileNameByMatchingEndpoint(ctx context.Context, opts ResolveProfileOptions) (string, error) {
	endpoint, err := opts.ResolveSetting(meshstack.EndpointSetting)
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
		slog.InfoContext(ctx, fmt.Sprintf("Using profile %s by uniquely matching endpoint '%s'", profile, endpoint))
		return string(profile.Name), nil
	}
	return "", nil
}
