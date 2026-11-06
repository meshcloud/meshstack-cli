package profile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func newDelete() *cobra.Command {
	yesFlag := internal.Flag[bool]{Name: "yes", Help: "delete the profile selected by --profile or --endpoint without asking for confirmation"}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a profile and its stored credentials",
		Long: `Delete a profile and its stored credentials. For a browser login, this also ends the login's session at meshStack, as meshstack auth logout does. Your browser stays signed in to meshStack.

Without --` + internal.ProfileFlag.Name.String() + `, it asks which of the profiles for the endpoint to delete. It asks for confirmation before it deletes. Use --yes to skip the confirmation; --yes needs --` + internal.ProfileFlag.Name.String() + `, or an --` + internal.EndpointFlag.Name.String() + ` that matches exactly one profile.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if yesFlag.Value && !cmd.Flags().Changed(internal.ProfileFlag.Name.String()) && !cmd.Flags().Changed(internal.EndpointFlag.Name.String()) {
				return fmt.Errorf("--%s needs --%s, or an --%s that matches exactly one profile", yesFlag.Name, internal.ProfileFlag.Name, internal.EndpointFlag.Name)
			}
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			return withLockedProfiles(ctx, func(profiles profile.Profiles) error {
				return deleteProfile(ctx, cmd, p, profiles, yesFlag.Value)
			})
		},
	}
	yesFlag.Register(cmd.Flags())
	return cmd
}

func deleteProfile(ctx context.Context, cmd *cobra.Command, p prompt.Prompt, profiles profile.Profiles, yes bool) error {
	deleted, named, err := selectProfile(ctx, cmd, p, profiles)
	if err != nil {
		return err
	}
	if !yes || !named {
		confirmed, err := p.Confirm(ctx, fmt.Sprintf("Delete profile '%s' and its stored credentials?", deleted.Name))
		if err != nil {
			return err
		}
		if !confirmed {
			slog.InfoContext(ctx, fmt.Sprintf("Kept profile '%s'.", deleted.Name))
			return nil
		}
	}
	if err := remove(ctx, &profiles, deleted.Name); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("Deleted profile '%s'.", deleted.Name))
	return nil
}
