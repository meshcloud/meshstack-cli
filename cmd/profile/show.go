package profile

import (
	"context"
	_ "embed"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

//go:embed show.md.tmpl
var showTemplateText string

var showTemplate = markdown.Parse("show", showTemplateText)

// entry is a profile as list and show write it, with the keys profiles.json has.
type entry struct {
	*profile.Profile `json:",inline"`

	Name    profile.Name `json:"name,omitzero"`
	Current bool         `json:"current"`
}

func entryOf(profiles profile.Profiles, p *profile.Profile) entry {
	return entry{Name: p.Name, Profile: p, Current: p.Name == profiles.CurrentProfile}
}

type shown struct {
	entry `json:",inline"`

	New bool `json:"new,omitzero"`

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
		Long: `Show the profile that other commands would use, and the status of its stored credential. Flags and environment variables can override that credential.

If the command uses no stored profile for the endpoint, it shows the endpoint alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			shownProfile, stored, err := profile.ResolveProfile(ctx, internal.SettingSources())
			if err != nil {
				return err
			}
			s := showOf(ctx, stored, shownProfile)
			s.New = shownProfile.Name == ""
			return output.Show(cmd.OutOrStdout(), showTemplate, s)
		},
	}
	output.Register(cmd.Flags())
	return cmd
}
