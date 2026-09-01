package profile

import (
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
)

// DefaultName is the bottom of the profile setting's ranked list, and where every front end
// lands when nothing names a profile.
const DefaultName = "default"

// Summary is one entry of config.json as a listing shows it, for the prompt that asks which
// endpoint a new profile belongs to.
type Summary struct {
	Name             string
	Endpoint         string
	DefaultWorkspace string
	IsCurrent        bool
}

// List returns the configured profiles, sorted by name.
func List() ([]Summary, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	known := make([]Summary, 0, len(config.Profiles))
	for name, entry := range config.Profiles {
		endpoint := ""
		if entry.Endpoint != nil {
			endpoint = entry.Endpoint.String()
		}
		known = append(known, Summary{
			Name:             name,
			Endpoint:         endpoint,
			DefaultWorkspace: entry.DefaultWorkspace,
			IsCurrent:        name == config.CurrentProfile,
		})
	}
	slices.SortFunc(known, func(a, b Summary) int {
		if a.Name < b.Name {
			return -1
		} else if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return known, nil
}

// Ensure creates the profile when it does not exist, and is the only thing that ever creates
// one: a mistyped --profile on an ordinary command must report an unknown profile rather than
// leave one behind with no endpoint, so the single caller is `meshstack auth login`, whose
// purpose is to configure.
func Ensure(name string, endpoint *xurl.URL) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if config.Profiles == nil {
		config.Profiles = map[string]Profile{}
	}
	entry, exists := config.Profiles[name]
	if exists && (endpoint == nil || (entry.Endpoint != nil && meshstack.SameEndpoint(*entry.Endpoint, *endpoint))) {
		return nil
	}
	if endpoint == nil {
		return diags.Errorf("no endpoint for a new profile",
			"profile %q does not exist. Name its endpoint with --endpoint or %s.", name, meshstack.Endpoint.EnvKey)
	}
	entry.Endpoint = endpoint
	config.Version = Version
	config.Profiles[name] = entry
	if config.CurrentProfile == "" {
		config.CurrentProfile = name
	}
	return SaveConfig(config)
}

// SetEndpoint and SetWorkspace back `meshstack profile set`. They refuse an unknown name for
// the same reason Ensure is the only creator.
func SetEndpoint(name, endpoint string) error {
	return update(name, func(entry *Profile) error {
		parsed, err := meshstack.ParseEndpoint(endpoint)
		if err != nil {
			return err
		}
		entry.Endpoint = &parsed
		return nil
	})
}

func SetWorkspace(name, ws string) error {
	return update(name, func(entry *Profile) error {
		entry.DefaultWorkspace = ws
		return nil
	})
}

func update(name string, change func(*Profile) error) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	entry, ok := config.Profiles[name]
	if !ok {
		return diags.Errorf("unknown profile",
			"profile %q is not in %s. `meshstack auth login --profile %s` creates it.", name, DescribeConfigPath(), name)
	}
	if err := change(&entry); err != nil {
		return err
	}
	config.Version = Version
	config.Profiles[name] = entry
	return SaveConfig(config)
}

// DescribeConfigPath names config.json inside a message about something else, so a path that
// cannot be resolved degrades to a description rather than replacing that message with its own.
func DescribeConfigPath() string {
	path, err := ConfigPath()
	if err != nil {
		return "the meshStack CLI configuration"
	}
	return path
}
