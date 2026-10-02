package profile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type (
	SettingSources        = setting.Sources
	ResolveProfileOptions struct {
		SettingSources

		// EndpointOptional lets a profile that ResolveProfile creates go without an endpoint, for a
		// command that only shows it.
		EndpointOptional bool
		// StoredOnly makes ResolveProfile return ErrNoStoredProfile rather than create a profile, for a
		// command that only acts on a stored one.
		StoredOnly bool
		// ExclusiveLock is LoadProfilesOptions.ExclusiveLock for the profiles ResolveProfile
		// returns, and holds nothing where it fails.
		ExclusiveLock bool
	}
)

var ErrNoStoredProfile = errors.New("no stored profile")

// ResolveProfile returns a *Profile that points into Profiles.Profiles, so a change through the
// pointer is persisted by a later Profiles.Store.
func ResolveProfile(ctx context.Context, opts ResolveProfileOptions) (*Profile, Profiles, error) {
	var loaded Profiles
	current, profiles, err := resolveProfile(ctx, opts, sync.OnceValues(func() (Profiles, error) {
		var err error
		loaded, err = LoadProfiles(ctx, LoadProfilesOptions{SettingSources: opts.SettingSources, ExclusiveLock: opts.ExclusiveLock})
		return loaded, err
	}))
	if err != nil {
		err = errors.Join(err, loaded.Unlock())
	}
	return current, profiles, err
}

func resolveProfile(ctx context.Context, opts ResolveProfileOptions, loadProfiles func() (Profiles, error)) (*Profile, Profiles, error) {
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

	// A front end source that selects the profile reads what to select from out of the context,
	// as the one for the workspace does. Being a fallback source of the front end, it ranks above
	// both of the sources here, so it has to take a unique endpoint match without asking.
	ctxWithSelection := SetSelectionInContext(ctx, sync.OnceValues(func() (Selection, error) {
		profiles, err := loadProfiles()
		if err != nil {
			return Selection{}, err
		}
		return profiles.SelectionFor(ctx, opts)
	}))

	name, err := opts.ResolveSetting(ctxWithSelection, NameSetting,
		endpointMatchingSource,
		currentProfileSource,
	)
	if err != nil {
		return nil, Profiles{}, err
	}

	profiles, err := loadProfiles()
	if err != nil {
		return nil, profiles, err
	}
	if currentProfile, ok := profiles.Profiles[name]; ok {
		return currentProfile, profiles, nil
	}
	if opts.StoredOnly {
		return nil, profiles, fmt.Errorf("%w is named '%s'", ErrNoStoredProfile, name)
	}
	if len(profiles.Profiles) == 0 {
		slog.InfoContext(ctx, fmt.Sprintf("Initializing first-time use profile '%s'", name))
	} else {
		slog.InfoContext(ctx, fmt.Sprintf("Creating profile '%s'", name))
	}
	created, err := addProfile(ctx, opts, &profiles, name)
	return created, profiles, err
}

func (ps Profiles) findProfileNameByMatchingEndpoint(ctx context.Context, opts ResolveProfileOptions) (string, error) {
	endpoint, found, err := opts.resolveEndpointIfAny(ctx)
	if err != nil || !found {
		return "", err
	}
	if matchingProfiles := ps.MatchingEndpoint(endpoint); len(matchingProfiles) == 1 {
		profile := matchingProfiles[0]
		slog.DebugContext(ctx, fmt.Sprintf("Using profile %s by uniquely matching endpoint '%s'", profile, endpoint))
		return string(profile.Name), nil
	}
	return "", nil
}

func (ps Profiles) SelectionFor(ctx context.Context, opts ResolveProfileOptions) (Selection, error) {
	selection := ps.Selection()
	endpoint, found, err := opts.resolveEndpointIfAny(ctx)
	if found {
		selection.Endpoint = &endpoint
	}
	return selection, err
}

func (opts ResolveProfileOptions) resolveEndpointIfAny(ctx context.Context) (xurl.URL, bool, error) {
	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting)
	if errors.Is(err, setting.ErrNoSourceProvidedValue) {
		slog.DebugContext(ctx, "No endpoint known at this point, cannot search for matching profile")
		return xurl.URL{}, false, nil
	}
	return endpoint, err == nil, err
}
