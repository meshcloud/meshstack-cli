package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

const (
	version = 1
)

type Profiles struct {
	Version        int               `json:"version"`
	CurrentProfile Name              `json:"currentProfile,omitzero"`
	Profiles       map[Name]*Profile `json:"profiles,omitzero"`

	configDir config.Directory
}

func LoadProfiles(ctx context.Context, opts ResolveProfileOptions) (profiles Profiles, err error) {
	profiles.configDir, err = opts.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return
	}
	err = json.UnmarshalFrom(ctx, profiles.configDir.ProfilesJson(), &profiles, json.ModifyAfterUnmarshal(func(target *map[Name]*Profile) {
		for name, profile := range *target {
			if profile == nil {
				continue // a null entry in the file; validate reports it
			}
			profile.init(name, profiles.configDir)
		}
	}))
	if errors.Is(err, fs.ErrNotExist) {
		err = initEmptyProfiles(ctx, opts, &profiles)
	} else if err == nil {
		err = profiles.validate()
	}
	return
}

func (ps Profiles) Store(ctx context.Context) error {
	return json.MarshalTo(ctx, ps.configDir.ProfilesJson(), ps)
}

func initEmptyProfiles(ctx context.Context, opts ResolveProfileOptions, profiles *Profiles) error {
	profiles.Version = version
	name, err := opts.ResolveSetting(ctx, NameSetting)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("Initializing first-time use profile '%s'", name))
	_, err = addProfile(ctx, opts, profiles, name)
	return err
}

// addProfile creates the named profile and makes it the current one, with the endpoint of this run
// where there is one.
func addProfile(ctx context.Context, opts ResolveProfileOptions, profiles *Profiles, name Name) (*Profile, error) {
	added := &Profile{}
	added.init(name, profiles.configDir)
	if profiles.Profiles == nil {
		profiles.Profiles = make(map[Name]*Profile, 1)
	}
	profiles.Profiles[name] = added
	profiles.CurrentProfile = name

	if endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting); err == nil {
		added.Endpoint = &endpoint
	} else if !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Profile %s resolved to %+v", name, *added))
	return added, nil
}

func (ps Profiles) validate() (err error) {
	if ps.Version != version {
		err = errors.Join(err, fmt.Errorf("version in %s mismatch %d vs expected %d", ps.configDir, ps.Version, version))
	}
	for name, profile := range ps.Profiles {
		if profile == nil {
			err = errors.Join(err, fmt.Errorf("profile '%s' in %s is null", name, ps.configDir))
		}
	}
	return
}
