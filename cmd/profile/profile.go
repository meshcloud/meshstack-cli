package profile

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage the stored profiles",
		Long: `Manage your profiles. A profile stores an endpoint, a credential and a default workspace.
meshstack login creates profiles as well.

In a terminal, this command opens an interactive list where you can add, edit and delete profiles,
and choose the current one. Outside a terminal, it shows this help. Use the subcommands there: list
and show print the profiles, and add, edit and delete ask one question per line.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// The list takes the whole screen, so it needs stdout to be the terminal as well.
			p := prompt.New(cmd.InOrStdin(), cmd.OutOrStdout())
			if !p.UsesTerminal() {
				return cmd.Help()
			}
			return withLockedProfiles(ctx, func(profiles profile.Profiles) error {
				selection, err := profiles.SelectionFor(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
				if err != nil {
					return err
				}
				m := newModel(ctx, profiles)
				if candidates := selection.Candidates(); selection.Endpoint != nil && len(candidates) > 0 {
					m = m.withRows(candidates[0].Name)
				}
				return run(ctx, p, m)
			})
		},
	}

	// The global --workspace selects the workspace of a run, which none of these commands has: add
	// and edit shadow it with defaultWorkspaceFlag, and the others refuse it.
	cmd.PersistentFlags().VarP(workspaceRefused{}, internal.WorkspaceFlag.Name.String(), internal.WorkspaceFlag.Shorthand, "")
	_ = cmd.PersistentFlags().MarkHidden(internal.WorkspaceFlag.Name.String())

	cmd.AddCommand(newList())
	cmd.AddCommand(newShow())
	cmd.AddCommand(newAdd())
	cmd.AddCommand(newEdit())
	cmd.AddCommand(newDelete())

	return cmd
}

type workspaceRefused struct{}

func (workspaceRefused) String() string { return "" }
func (workspaceRefused) Type() string   { return "string" }
func (workspaceRefused) Set(string) error {
	return fmt.Errorf("this command does not use --%s; to set the default workspace of a profile, run 'meshstack profile edit --%s <workspace>'",
		internal.WorkspaceFlag.Name, internal.WorkspaceFlag.Name)
}

func defaultWorkspaceFlag() internal.Flag[string] {
	return internal.Flag[string]{
		Name: internal.WorkspaceFlag.Name, Shorthand: internal.WorkspaceFlag.Shorthand,
		Help: "the default workspace of the profile",
	}
}
