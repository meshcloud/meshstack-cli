package auth

import (
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
)

// KnownProfile is one entry of config.json, for the prompt that asks which endpoint a new
// profile belongs to.
type KnownProfile struct {
	Name             string
	Endpoint         string
	DefaultWorkspace string
	IsCurrent        bool
}

// KnownProfiles lists the configured profiles, sorted by name.
func KnownProfiles() ([]KnownProfile, error) {
	config, err := profile.LoadConfig()
	if err != nil {
		return nil, err
	}
	known := make([]KnownProfile, 0, len(config.Profiles))
	for name, entry := range config.Profiles {
		known = append(known, KnownProfile{
			Name:             name,
			Endpoint:         endpointString(entry.Endpoint),
			DefaultWorkspace: entry.DefaultWorkspace,
			IsCurrent:        name == config.CurrentProfile,
		})
	}
	slices.SortFunc(known, func(a, b KnownProfile) int {
		if a.Name < b.Name {
			return -1
		} else if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return known, nil
}

// EnsureProfile creates the profile when it does not exist, and is the only thing that ever
// creates one. A mistyped --profile on an ordinary command reports an unknown profile instead
// of quietly creating one with no endpoint, which is why `meshstack auth login` is the single
// caller: a command whose purpose is to configure is the one place where creating something
// is expected.
func EnsureProfile(name string, endpoint *xurl.URL) error {
	config, err := profile.LoadConfig()
	if err != nil {
		return err
	}
	if config.Profiles == nil {
		config.Profiles = map[string]profile.Profile{}
	}
	entry, exists := config.Profiles[name]
	if exists && (endpoint == nil || (entry.Endpoint != nil && meshstack.SameEndpoint(*entry.Endpoint, *endpoint))) {
		return nil
	}
	if endpoint == nil {
		return diags.Errorf("no endpoint for a new profile",
			"profile %q does not exist. Name its endpoint with --endpoint or %s.", name, envEndpoint)
	}
	entry.Endpoint = endpoint
	config.Version = profile.Version
	config.Profiles[name] = entry
	if config.CurrentProfile == "" {
		config.CurrentProfile = name
	}
	return profile.SaveConfig(config)
}

// SetProfileEndpoint and SetProfileWorkspace back `meshstack profile set`. They refuse an
// unknown name for the same reason EnsureProfile is the only creator.
func SetProfileEndpoint(name, endpoint string) error {
	return updateProfile(name, func(entry *profile.Profile) error {
		parsed, err := meshstack.ParseEndpoint(endpoint)
		if err != nil {
			return err
		}
		entry.Endpoint = &parsed
		return nil
	})
}

func SetProfileWorkspace(name, ws string) error {
	return updateProfile(name, func(entry *profile.Profile) error {
		entry.DefaultWorkspace = ws
		return nil
	})
}

func updateProfile(name string, change func(*profile.Profile) error) error {
	config, err := profile.LoadConfig()
	if err != nil {
		return err
	}
	entry, ok := config.Profiles[name]
	if !ok {
		return diags.Errorf("unknown profile",
			"profile %q is not in %s. `meshstack auth login --profile %s` creates it.", name, describeConfigPath(), name)
	}
	if err := change(&entry); err != nil {
		return err
	}
	config.Version = profile.Version
	config.Profiles[name] = entry
	return profile.SaveConfig(config)
}

// endpointString renders an optional URL for a status view, where an absent one reads as
// the empty string rather than as a missing field.
func endpointString(u *xurl.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}
