package profile

import (
	"context"
	"errors"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type SettingSources = setting.Sources

func ResolveProfile(ctx context.Context, sources SettingSources) (*Profile, Profiles, error) {
	profiles, err := LoadProfiles(ctx, LoadProfilesOptions{SettingSources: sources})
	if err != nil {
		return nil, profiles, err
	}
	resolved, err := profiles.Resolve(ctx, sources, false)
	return resolved, profiles, err
}

func (ps *Profiles) Resolve(ctx context.Context, sources SettingSources, forLogin bool) (*Profile, error) {
	selection, err := ps.SelectionFor(ctx, sources)
	if err != nil {
		return nil, err
	}
	nameSetting := NameSetting
	if forLogin {
		nameSetting.Default = setting.StaticDefault("default")
	}
	name, err := sources.ResolveSetting(SetSelectionInContext(ctx, selection), nameSetting, selection.nameSource())
	if err != nil && !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return nil, err
	}
	stored, isStored := ps.Profiles[name]
	switch {
	case isStored && selection.Endpoint != nil && !selection.Endpoint.Equal(stored.Endpoint):
		return nil, fmt.Errorf("profile '%s' has the endpoint %s, which does not match the endpoint %s of this command; "+
			"use --profile to choose a profile of that endpoint, or run '%s'", name, stored.Endpoint, selection.Endpoint, ps.LoginHint("", *selection.Endpoint))
	case isStored:
		return stored, nil
	case name != "" && !forLogin:
		return nil, fmt.Errorf("there is no profile '%s'; run '%s' to create it", name, ps.LoginHint(name, xurl.URL{}))
	case selection.Endpoint == nil:
		_, noEndpoint := sources.ResolveSetting(ctx, meshstack.EndpointSetting)
		if forLogin {
			return nil, fmt.Errorf("the new profile '%s' needs an endpoint: %w", name, noEndpoint)
		}
		return nil, errors.Join(noEndpoint, fmt.Errorf("or run '%s'", ps.LoginHint("", xurl.URL{})))
	case forLogin:
		return ps.add(Profile{Name: name, Endpoint: *selection.Endpoint}), nil
	}
	return &Profile{Endpoint: *selection.Endpoint, ConfigDir: ps.configDir}, nil
}

func (ps *Profiles) SelectionFor(ctx context.Context, sources SettingSources) (Selection, error) {
	selection := ps.Selection()
	endpoint, err := sources.ResolveSetting(ctx, meshstack.EndpointSetting)
	switch {
	case err == nil:
		selection.Endpoint = &endpoint
	case !errors.Is(err, setting.ErrNoSourceProvidedValue):
		return selection, err
	}
	return selection, nil
}
