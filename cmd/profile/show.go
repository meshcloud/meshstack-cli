package profile

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
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

	// New is the profile a first login would create, which is not stored yet.
	New bool `json:"new,omitzero"`

	Status *auth.CredentialStatus `json:"status,omitzero"`
	// StatusError says why there is no status for a stored credential.
	StatusError string `json:"statusError,omitzero"`
}

// statusReadTime is shorter than a command waits for an unreachable meshStack, which is about 30s,
// as edit reads the status before it asks anything.
const statusReadTime = 5 * time.Second

// showOf has no status where p stores no usable credential, and logs a warning for it, unless p was
// never logged in, which show.md.tmpl says itself.
func showOf(ctx context.Context, profiles profile.Profiles, p *profile.Profile) shown {
	s := shown{entry: entryOf(profiles, p)}
	if p.Credential == "" {
		return s
	}
	ctx, cancel := context.WithTimeoutCause(ctx, statusReadTime, fmt.Errorf("meshStack did not answer within %s", statusReadTime))
	defer cancel()
	session, err := auth.StoredSession(ctx, p, internal.ResolveClientOptions())
	if err != nil {
		slog.WarnContext(ctx, "Cannot read the credential of profile '"+string(p.Name)+"': "+err.Error())
		return s
	}
	status := session.Status(ctx)
	// Status reads what it can, and logs what it cannot, so a status read past the deadline lacks
	// what meshStack would have answered.
	if ctx.Err() != nil {
		s.StatusError = context.Cause(ctx).Error()
		return s
	}
	s.Status = &status
	return s
}

func newShow() *cobra.Command {
	var output internal.ShowFlag
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a profile and the status of its stored credential",
		Long: `Show the profile another command would use, and the status of the credential it stores, which
a flag or the environment would replace there.

A profile that is not stored yet is shown as a first login would create it, and nothing is stored.
Where meshStack does not answer within ` + statusReadTime.String() + `, it shows what the profile stores without the status.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			opts := profile.ResolveProfileOptions{SettingSources: internal.SettingSources(), EndpointOptional: true}
			stored, err := profile.LoadProfiles(ctx, profile.LoadProfilesOptions{SettingSources: opts.SettingSources})
			if err != nil {
				return err
			}
			shownProfile, _, err := profile.ResolveProfile(ctx, opts)
			if err != nil {
				return err
			}
			if endpoint, source, err := opts.ResolveSettingWithSource(ctx, meshstack.EndpointSetting); err == nil && !endpoint.Equal(shownProfile.Endpoint) {
				slog.WarnContext(ctx, fmt.Sprintf("Profile '%s' is for endpoint '%s', so a command for endpoint '%s' (from %s) fails with it",
					shownProfile.Name, shownProfile.Endpoint, endpoint, source.Describe(meshstack.EndpointSetting.EnvKey())))
			}
			s := showOf(ctx, stored, shownProfile)
			_, isStored := stored.Profiles[shownProfile.Name]
			s.New = !isStored
			return output.Show(cmd.OutOrStdout(), showTemplate, s)
		},
	}
	output.Register(cmd.Flags())
	return cmd
}
