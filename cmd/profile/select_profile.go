package profile

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

// selectProfile returns namedByFlag only where a flag alone picked the profile: a MESHSTACK_PROFILE
// or MESHSTACK_ENDPOINT left in the environment is no answer to which profile to change.
func selectProfile(ctx context.Context, cmd *cobra.Command, p prompt.Prompt, profiles profile.Profiles) (selected *profile.Profile, namedByFlag bool, err error) {
	if cmd.Flags().Changed(internal.ProfileFlag.Name.String()) {
		selected, err = find(profiles, profile.Name(internal.ProfileFlag.Value))
		return selected, true, err
	}
	selection, err := profiles.SelectionFor(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
	if err != nil {
		return nil, false, err
	}
	candidates := selection.Candidates()
	if selection.Endpoint != nil && len(candidates) == 0 {
		return nil, false, fmt.Errorf("no profile is for endpoint '%s'", selection.Endpoint)
	}
	selected, err = prompt.Select(ctx, p, "profile", candidates, func(candidate *profile.Profile) bool {
		return candidate.Name == selection.Current
	})
	endpointFlagGiven := cmd.Flags().Changed(internal.EndpointFlag.Name.String())
	return selected, endpointFlagGiven && len(candidates) == 1, err
}
