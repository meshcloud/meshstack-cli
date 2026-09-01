package profile

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// Selection is which profile this run uses, and what config.json says about it.
type Selection struct {
	Name   string
	Entry  Profile
	Exists bool

	// Named records that a source above config.json asked for this profile by name. It is
	// what tells a typo ("unknown profile") from a machine nobody has configured ("no
	// profile for this endpoint"), which are different messages for the same Exists.
	Named bool

	// Endpoint is what a source above the profile named, empty when none did. The caller
	// finishes the endpoint's ranked list with the profile's own.
	Endpoint string

	Origins []setting.Origin
}

// Select applies the profile layer of the ranked order: a name from the sources it is
// handed, then the only profile configured for the endpoint those sources name, then
// currentProfile, then DefaultName.
//
// Exists is reported rather than judged. Whether a profile that does not exist yet is an
// error depends on whether the command is about to write it, and only the caller knows that.
//
// It takes a context because it warns from here rather than reporting the fact upward: the
// endpoint match is decided in this function, and `meshstack profile set` calls Select
// without ever building the session that would otherwise phrase the warning.
func Select(ctx context.Context, sources ...setting.Source) (Selection, error) {
	config, err := LoadConfig()
	if err != nil {
		return Selection{}, err
	}

	endpoint, endpointFrom, err := setting.Resolve(meshstack.Endpoint, sources...)
	if err != nil {
		return Selection{}, err
	}
	name, nameFrom, err := setting.Resolve(Name, sources...)
	if err != nil {
		return Selection{}, err
	}

	selection := Selection{Name: name, Named: nameFrom != nil}
	if nameFrom != nil {
		selection.Origins = append(selection.Origins, setting.Origin{
			Key: Name.EnvKey, Source: nameFrom.Describe(Name.EnvKey),
		})
	} else {
		// An endpoint given on its own is almost always meant as "the instance I have a
		// profile for", so resolving it is friendlier than refusing.
		var matches []string
		if endpointFrom != nil {
			for candidate, entry := range config.Profiles {
				if entry.Endpoint != nil && meshstack.SameEndpoint(*entry.Endpoint, endpoint) {
					matches = append(matches, candidate)
				}
			}
			slices.Sort(matches)
		}

		source := ""
		switch {
		case len(matches) > 1:
			for i, match := range matches {
				matches[i] = strconv.Quote(match)
			}
			return Selection{}, diags.Errorf("several profiles match this endpoint",
				"%s are all configured for %s. Pick one with --profile.", strings.Join(matches, ", "), endpoint)
		case len(matches) == 1:
			selection.Name, source = matches[0], "the only profile for "+endpoint.String()
			// A terraform plan whose identity depends on which profiles exist on the machine
			// should at least announce it.
			slog.WarnContext(ctx, "picked a profile by endpoint",
				"detail", fmt.Sprintf("profile %q is the only one configured for %s, so this command uses its credentials. Name one with --profile to be explicit.",
					selection.Name, endpoint))
		case config.CurrentProfile != "":
			selection.Name, source = config.CurrentProfile, "currentProfile in "+DescribeConfigPath()
		default:
			selection.Name, source = DefaultName, "built-in default"
		}
		selection.Origins = append(selection.Origins, setting.Origin{Key: Name.EnvKey, Source: source})
	}

	if endpointFrom != nil {
		selection.Endpoint = endpoint.String()
		selection.Origins = append(selection.Origins, setting.Origin{
			Key: meshstack.Endpoint.EnvKey, Source: endpointFrom.Describe(meshstack.Endpoint.EnvKey),
		})
	}
	selection.Entry, selection.Exists = config.Profiles[selection.Name]
	return selection, nil
}
