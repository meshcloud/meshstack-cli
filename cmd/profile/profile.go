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
		Long: `Manage the profiles that meshstack auth login stores, each an endpoint with its credential and
default workspace.

On a terminal, this lists the profiles to add, edit, delete, or make the current one, starting at
the first one for the endpoint where --endpoint or MESHSTACK_ENDPOINT gives one. Elsewhere it shows
this help: list and show print the profiles, and add, edit and delete ask line by line.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// The list takes the whole screen, so it needs stdout to be the terminal as well.
			p := prompt.New(cmd.InOrStdin(), cmd.OutOrStdout())
			if !p.UsesTerminal() {
				return cmd.Help()
			}
			profiles, err := profile.LoadProfiles(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
			if err != nil {
				return err
			}
			selection, err := profiles.SelectionFor(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
			if err != nil {
				return err
			}
			m := newModel(ctx, profiles)
			if candidates := selection.Candidates(); selection.Endpoint != nil && len(candidates) > 0 {
				m = m.withRows(candidates[0].Name)
			}
			return run(ctx, p, m)
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
	return fmt.Errorf("has no effect here; set the default workspace of a profile with 'meshstack profile edit --%s'", internal.WorkspaceFlag.Name)
}

func defaultWorkspaceFlag() internal.Flag[string] {
	return internal.Flag[string]{
		Name: internal.WorkspaceFlag.Name, Shorthand: internal.WorkspaceFlag.Shorthand,
		Help: "the default workspace of the profile",
	}
}
