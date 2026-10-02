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
	unlock    func() error
}

type LoadProfilesOptions struct {
	SettingSources

	// ExclusiveLock holds the profiles until Profiles.Unlock, for a command that stores them, so
	// that no other such command stores over what this one changes. Where another one holds them,
	// LoadProfiles fails with ErrInUse within lockWaitTime.
	ExclusiveLock bool
}

// LoadProfiles only reads, and resolves nothing but the configuration directory: ResolveProfile
// creates the first profile, as it creates any other.
func LoadProfiles(ctx context.Context, opts LoadProfilesOptions) (profiles Profiles, err error) {
	profiles.configDir, err = opts.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return
	}
	if opts.ExclusiveLock {
		if profiles.unlock, err = lockExclusively(ctx, profiles.configDir); err != nil {
			return
		}
		defer func() {
			if err != nil {
				err = errors.Join(err, profiles.Unlock())
			}
		}()
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
		profiles.Version, err = version, nil
	} else if err == nil {
		err = profiles.validate()
	}
	return
}

func (ps *Profiles) Store(ctx context.Context) error {
	return json.MarshalTo(ctx, ps.configDir.ProfilesJson(), ps)
}

// Unlock releases the lock of LoadProfilesOptions.ExclusiveLock, and does nothing for profiles
// loaded without it or unlocked already.
func (ps *Profiles) Unlock() error {
	if ps.unlock == nil {
		return nil
	}
	return ps.unlock()
}

func (ps *Profiles) Add(p Profile) *Profile {
	p.init(p.Name, ps.configDir)
	if ps.Profiles == nil {
		ps.Profiles = make(map[Name]*Profile, 1)
	}
	ps.Profiles[p.Name] = &p
	return &p
}

func (ps *Profiles) addCurrent(ctx context.Context, opts ResolveProfileOptions, name Name) (*Profile, error) {
	added := ps.Add(Profile{Name: name})
	ps.CurrentProfile = name

	endpoint, err := opts.ResolveSetting(ctx, meshstack.EndpointSetting)
	if opts.EndpointOptional && errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return added, nil
	} else if err != nil {
		return nil, err
	}
	added.Endpoint = endpoint

	slog.DebugContext(ctx, fmt.Sprintf("Profile %s resolved to %+v", name, *added))
	return added, nil
}

func (ps *Profiles) validate() (err error) {
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
