package profile

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newEdit() *cobra.Command {
	workspaceFlag := defaultWorkspaceFlag()
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a profile",
		Long: `Edit a profile. In a terminal, a form opens; otherwise, the command asks one question per line.
Leave an answer empty to keep the current value. Without --` + internal.ProfileFlag.Name.String() + `, it asks which of the profiles
for the endpoint to edit. --` + internal.WorkspaceFlag.Name.String() + ` pre-fills the default workspace.

Changing the endpoint deletes the stored credentials.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			return withLockedProfiles(ctx, func(profiles profile.Profiles) error {
				edited, _, err := selectProfile(ctx, cmd, p, profiles)
				if err != nil {
					return err
				}
				d := draftOf(edited)
				if cmd.Flags().Changed(workspaceFlag.Name.String()) {
					d.workspace = workspaceFlag.Value
				}
				return edit(ctx, p, profiles, d)
			})
		},
	}
	workspaceFlag.Register(cmd.Flags())
	return cmd
}

func edit(ctx context.Context, p prompt.Prompt, profiles profile.Profiles, d draft) error {
	if !p.UsesTerminal() {
		if d.original != nil {
			details, err := markdown.Execute(showTemplate, showOf(ctx, profiles, d.original))
			if err != nil {
				return err
			}
			if err := p.Printf("%s\n", details); err != nil {
				return err
			}
		}
		if err := d.ask(ctx, p, profiles); err != nil {
			return err
		}
		done, err := d.save(ctx, &profiles)
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, done)
		return nil
	}
	m, _ := newModel(ctx, profiles).withForm(d) //nolint:contextcheck // Update gets no context, so the model carries ctx
	m.quitAfterForm = true
	return run(ctx, p, m)
}
