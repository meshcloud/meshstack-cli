package profile

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
)

type Selection struct {
	Name   Name
	Entry  Profile
	Exists bool

	// Named tells a typo from a machine nobody has configured, for the same Exists.
	Named bool

	// Endpoint is what a source above the profile named, empty when none did.
	Endpoint string

	NameFrom     setting.SourceDescription
	EndpointFrom setting.SourceDescription
}

// Select resolves the profile name from the given sources, then the only profile configured
// for the endpoint they name, then currentProfile, then DefaultName. Exists is reported
// rather than judged: only the caller knows whether it is about to write the profile.
func Select(ctx context.Context, sources ...setting.Source) (Selection, error) {
	config, err := LoadConfig()
	if err != nil {
		return Selection{}, err
	}

	endpoint, endpointResolution, err := setting.Resolve(meshstack.Endpoint, sources...)
	if err != nil {
		return Selection{}, err
	}
	name, nameResolution, err := setting.Resolve(NameSetting, sources...)
	if err != nil {
		return Selection{}, err
	}

	selection := Selection{Name: name, Named: nameResolution.From != nil}
	if from := nameResolution.From; from != nil {
		selection.NameFrom = from.Describe(NameSetting.EnvKey())
	} else {
		var matches []Name
		if endpointResolution.From != nil {
			for candidate, entry := range config.Profiles {
				if entry.Endpoint != nil && entry.Endpoint.Equal(endpoint) {
					matches = append(matches, candidate)
				}
			}
			slices.Sort(matches)
		}

		switch {
		case len(matches) > 1:
			quoted := make([]string, len(matches))
			for i, match := range matches {
				quoted[i] = strconv.Quote(string(match))
			}
			return Selection{}, fmt.Errorf("several profiles match this endpoint: %s are all configured for %s. Name one with %s",
				strings.Join(quoted, ", "), endpoint, NameSetting.EnvKey())
		case len(matches) == 1:
			selection.Name = matches[0]
			selection.NameFrom = setting.SourceDescription{Type: "the only profile for", Details: endpoint.String()}
			slog.WarnContext(ctx, "picked a profile by endpoint",
				"detail", fmt.Sprintf("profile %q is the only one configured for %s, so this command uses its credentials. Name one with %s to be explicit.",
					selection.Name, endpoint, NameSetting.EnvKey()))
		case config.CurrentProfile != "":
			selection.Name = config.CurrentProfile
			selection.NameFrom = setting.SourceDescription{Type: "currentProfile in", Details: DescribeConfigPath()}
		default:
			selection.Name = DefaultName
			selection.NameFrom = setting.SourceDescription{Type: "built-in default"}
		}
	}

	if from := endpointResolution.From; from != nil {
		selection.Endpoint = endpoint.String()
		selection.EndpointFrom = from.Describe(meshstack.Endpoint.EnvKey())
	}
	selection.Entry, selection.Exists = config.Profiles[selection.Name]
	return selection, nil
}
