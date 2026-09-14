package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

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

	configDir ConfigDirectory
}

func LoadProfiles(ctx context.Context, opts ResolveProfileOptions) (profiles Profiles, err error) {
	// config dir always resolves, as ConfigDirectorySetting has a default.
	profiles.configDir, err = opts.ResolveSetting(ConfigDirectorySetting)
	if err != nil {
		return
	}
	err = json.UnmarshalFrom(ctx, profiles.configDir.ProfilesJson(), &profiles, json.ModifyAfterUnmarshal(func(target *map[Name]*Profile) {
		// init Profile map entries after unmarshal
		for name, profile := range *target {
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
	// Profile name always resolves, as NameSetting has a (static) default.
	if name, err := opts.ResolveSetting(NameSetting); err != nil {
		return err
	} else {
		slog.InfoContext(ctx, fmt.Sprintf("Initializing first-time use profile '%s'", name))
		profiles.CurrentProfile = name
	}

	currentProfile := &Profile{}
	currentProfile.init(profiles.CurrentProfile, profiles.configDir)
	profiles.Profiles = map[Name]*Profile{profiles.CurrentProfile: currentProfile}

	if endpoint, err := opts.ResolveSetting(meshstack.EndpointSetting); err == nil {
		currentProfile.Endpoint = &endpoint
	} else if !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Initial profile %s resolved to %+v", profiles.CurrentProfile, *currentProfile))
	return nil
}

func (ps Profiles) validate() (err error) {
	if ps.Version != version {
		err = errors.Join(err, fmt.Errorf("version in %s mismatch %d vs expected %d", ps.configDir, ps.Version, version))
	}
	return
}
