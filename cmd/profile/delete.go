package profile

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newDelete() *cobra.Command {
	yesFlag := internal.Flag[bool]{Name: "yes", Help: "delete the profile --profile or --endpoint names without asking"}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a profile and its stored credentials",
		Long: `Delete the profile --profile names, or the one selected from a list without it, together
with its stored credentials. The list offers only the profiles for the endpoint where --endpoint or
MESHSTACK_ENDPOINT gives one.

It asks before it deletes, unless --yes goes with --profile, or with an --endpoint that only one
profile is for.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if yesFlag.Value && !cmd.Flags().Changed(internal.ProfileFlag.Name.String()) && !cmd.Flags().Changed(internal.EndpointFlag.Name.String()) {
				return fmt.Errorf("--%s deletes only the profile --%s or --%s names", yesFlag.Name, internal.ProfileFlag.Name, internal.EndpointFlag.Name)
			}
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			profiles, err := profile.LoadProfiles(ctx, profile.ResolveProfileOptions{SettingSources: internal.SettingSources()})
			if err != nil {
				return err
			}
			deleted, named, err := selectProfile(cmd, p, profiles)
			if err != nil {
				return err
			}
			if !yesFlag.Value || !named {
				if err := p.Printf("%s", deleteQuestion(deleted.Name)); err != nil {
					return err
				}
				answer, err := p.Next(ctx, "confirmation")
				if err != nil {
					return err
				}
				if !confirmed(answer) {
					slog.InfoContext(ctx, fmt.Sprintf("Kept profile '%s'.", deleted.Name))
					return nil
				}
			}
			if err := remove(ctx, &profiles, deleted.Name); err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("Deleted profile '%s'.", deleted.Name))
			return nil
		},
	}
	yesFlag.Register(cmd.Flags())
	return cmd
}

func deleteQuestion(name profile.Name) string {
	return fmt.Sprintf("Delete profile '%s' and its stored credentials? [y/N]: ", name)
}

func confirmed(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
