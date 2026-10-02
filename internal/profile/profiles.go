package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"

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

func (ps *Profiles) Find(name Name) (*Profile, error) {
	found, ok := ps.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("there is no profile '%s'", name)
	}
	return found, nil
}

// CheckNameFree lets original keep its own name, and takes a nil original for a new profile.
func (ps *Profiles) CheckNameFree(name Name, original *Profile) error {
	if _, taken := ps.Profiles[name]; taken && (original == nil || original.Name != name) {
		return fmt.Errorf("a profile named '%s' exists already", name)
	}
	return nil
}

// Put stores edited in place of original, or adds it where original is nil.
func (ps *Profiles) Put(ctx context.Context, original *Profile, edited Profile) error {
	if err := ps.CheckNameFree(edited.Name, original); err != nil {
		return err
	}
	if original != nil {
		if err := original.moveCredentialsTo(ctx, edited); err != nil {
			return err
		}
		if !original.Endpoint.Equal(edited.Endpoint) {
			edited.Credential = ""
		}
		delete(ps.Profiles, original.Name)
		if ps.CurrentProfile == original.Name {
			ps.CurrentProfile = edited.Name
		}
	}
	ps.add(edited)
	if ps.CurrentProfile == "" {
		ps.CurrentProfile = edited.Name
	}
	return ps.Store(ctx)
}

func (ps *Profiles) Remove(ctx context.Context, name Name) (currentChanged bool, err error) {
	removed, err := ps.Find(name)
	if err != nil {
		return false, err
	}
	if err := removed.RemoveCredentials(ctx); err != nil {
		return false, err
	}
	delete(ps.Profiles, name)
	if ps.CurrentProfile != name {
		return false, ps.Store(ctx)
	}
	// Of several profiles left, none is picked: the current one is what a command uses without
	// asking, so the user picks it.
	left := ps.Selection().Profiles
	ps.CurrentProfile = ""
	if len(left) == 1 {
		ps.CurrentProfile = left[0].Name
	}
	return true, ps.Store(ctx)
}

func (ps *Profiles) SetCurrent(ctx context.Context, name Name) error {
	if _, err := ps.Find(name); err != nil {
		return err
	}
	ps.CurrentProfile = name
	return ps.Store(ctx)
}

func (ps *Profiles) Endpoints() []string {
	var endpoints []string
	for _, p := range ps.Profiles {
		if p.Endpoint.URL != nil {
			endpoints = append(endpoints, p.Endpoint.String())
		}
	}
	slices.Sort(endpoints)
	return slices.Compact(endpoints)
}

func (ps *Profiles) add(p Profile) *Profile {
	p.init(p.Name, ps.configDir)
	if ps.Profiles == nil {
		ps.Profiles = make(map[Name]*Profile, 1)
	}
	ps.Profiles[p.Name] = &p
	return &p
}

func (ps *Profiles) addCurrent(ctx context.Context, opts ResolveProfileOptions, name Name) (*Profile, error) {
	added := ps.add(Profile{Name: name})
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
