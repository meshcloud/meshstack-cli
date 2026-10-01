package profile

import (
	_ "embed"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

//go:embed list.md.tmpl
var listTemplateText string

var listTemplate = markdown.Parse("list", listTemplateText)

func newList() *cobra.Command {
	var output internal.ShowFlag
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the stored profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profiles, err := profile.LoadProfiles(cmd.Context(), profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
			if err != nil {
				return err
			}
			entries := []entry{}
			for _, p := range profiles.Selection().Profiles {
				entries = append(entries, entryOf(profiles, p))
			}
			return output.Show(cmd.OutOrStdout(), listTemplate, entries)
		},
	}
	output.Register(cmd.Flags())
	return cmd
}
