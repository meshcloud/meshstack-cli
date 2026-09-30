package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// newProfileSelectionSource lets the person pick the profile to act on. It is a fallback source,
// so --profile and MESHSTACK_PROFILE are taken as given. It ranks above the endpoint match and the
// current profile of internal/profile, which is why it has to take a single candidate itself, as
// prompt.Select does. With --stdin it asks nothing at all, since stdin then carries the secret.
func newProfileSelectionSource(p prompt.Prompt, stdinCarriesSecret bool) setting.FallbackSource {
	return setting.FallbackLookupSource(setting.Profile.EnvKey(), "the profile selection",
		func(ctx context.Context) (string, error) {
			labelProfiles := func(profiles []*profile.Profile) string {
				labels := make([]string, 0, len(profiles))
				for _, p := range profiles {
					labels = append(labels, p.Label())
				}
				return strings.Join(labels, ", ")
			}

			selection, err := profile.SelectionFromContext(ctx)
			if err != nil {
				return "", err
			}
			candidates := selection.Candidates()
			isCurrent := func(candidate *profile.Profile) bool { return candidate.Name == selection.Current }
			switch {
			case len(selection.Profiles) == 0:
				return "", nil
			case len(candidates) == 0:
				// A profile for another endpoint would send its credential there.
				return "", fmt.Errorf("no profile is for endpoint '%s', only %s; "+
					"create a new profile for it with `meshstack login --%s %s --%s <new profile name>`",
					selection.Endpoint, labelProfiles(selection.Profiles),
					internal.EndpointFlag.Name, selection.Endpoint, internal.ProfileFlag.Name)
			case stdinCarriesSecret && len(candidates) > 1:
				if slices.ContainsFunc(candidates, isCurrent) {
					return string(selection.Current), nil
				}
				return "", fmt.Errorf("this login could go to any of %s; name one with --%s, as --%s leaves no stdin to ask on",
					labelProfiles(candidates), internal.ProfileFlag.Name, stdinFlagName)
			}
			selected, err := prompt.Select(ctx, p, "profile", candidates, isCurrent)
			if err != nil {
				return "", err
			}
			return string(selected.Name), nil
		})
}
