package profile

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

//go:embed show.md.tmpl
var showTemplateText string

var showTemplate = markdown.Parse("show", showTemplateText)

// entry is a profile as list and show write it, with the keys profiles.json has.
type entry struct {
	*profile.Profile `json:",inline"`

	Name    profile.Name `json:"name"`
	Current bool         `json:"current"`
}

func entryOf(profiles profile.Profiles, p *profile.Profile) entry {
	return entry{Name: p.Name, Profile: p, Current: p.Name == profiles.CurrentProfile}
}

type shown struct {
	entry `json:",inline"`

	Status *auth.CredentialStatus `json:"status,omitzero"`
}

// showOf has no status where p stores no usable credential, and logs a warning for it, unless p was
// never logged in, which show.md.tmpl says itself.
func showOf(ctx context.Context, profiles profile.Profiles, p *profile.Profile) shown {
	s := shown{entry: entryOf(profiles, p)}
	if p.Credential == "" {
		return s
	}
	session, err := auth.StoredSession(ctx, p, internal.ResolveClientOptions())
	if err != nil {
		slog.WarnContext(ctx, "Cannot read the credential of profile '"+string(p.Name)+"': "+err.Error())
		return s
	}
	status := session.Status(ctx)
	s.Status = &status
	return s
}

func newShow() *cobra.Command {
	var output internal.ShowFlag
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a profile and the status of its stored credential",
		Long: `Show the profile --profile names, and the status of the credential it stores, which a flag or
the environment would replace in another command.

Without --profile, on a terminal it asks which profile to show, offering only the profiles for the
endpoint where --endpoint or MESHSTACK_ENDPOINT gives one. Elsewhere, and with --output json, it asks
nothing and shows the current profile, or the only profile for that endpoint.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			profiles, err := profile.LoadProfiles(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
			if err != nil {
				return err
			}
			var shownProfile *profile.Profile
			if p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr()); p.UsesTerminal() && !output.Json() {
				shownProfile, _, err = selectProfile(cmd, p, profiles)
			} else {
				shownProfile, err = profileWithoutAsking(cmd, profiles)
			}
			if err != nil {
				return err
			}
			return output.Show(cmd.OutOrStdout(), showTemplate, showOf(ctx, profiles, shownProfile))
		},
	}
	output.Register(cmd.Flags())
	return cmd
}

func profileWithoutAsking(cmd *cobra.Command, profiles profile.Profiles) (*profile.Profile, error) {
	if cmd.Flags().Changed(internal.ProfileFlag.Name.String()) {
		return find(profiles, profile.Name(internal.ProfileFlag.Value))
	}
	selection, err := profiles.SelectionFor(cmd.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
	if err != nil {
		return nil, err
	}
	candidates := selection.Candidates()
	if current := slices.IndexFunc(candidates, func(p *profile.Profile) bool { return p.Name == selection.Current }); current >= 0 {
		return candidates[current], nil
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return nil, fmt.Errorf("no current profile to show among %d; name one with --%s", len(candidates), internal.ProfileFlag.Name)
}
