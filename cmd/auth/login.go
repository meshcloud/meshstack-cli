package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func NewLoginShortcut() *cobra.Command {
	cmd := newLogin(nil)
	cmd.Short += " (same as auth login)"
	return cmd
}

// NewLoginTo logs in to the profile name, whichever profile the flags and the environment name.
func NewLoginTo(name profile.Name) *cobra.Command {
	return newLogin(setting.Sources{setting.LookupSource(setting.Profile.EnvKey(), "the profile highlighted in meshstack profile",
		func(context.Context) (string, error) { return string(name), nil })})
}

func newLogin(profileSources setting.Sources) *cobra.Command {
	var (
		openStdinFlag = newStdinFlag()
		apiKeyFlag    = internal.NewFlagForSetting[uuid.UUID]("apikey", setting.ApiKeyClientId)
		apiTokenFlag  = internal.NewFlagForSetting[bool]("apitoken", setting.ApiToken)
		output        internal.ShowFlag
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Long: `Log in to meshStack and store the credential in a profile, creating that profile where it
does not exist yet.

Without a profile named, it asks which stored profile to log in to, among those for the endpoint.
With no flag this is a browser login, and it asks which workspace to work in unless one is named.

It ends with what meshstack auth status shows for the new login.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if cmd.Flags().Changed(apiKeyFlag.Name.String()) {
				return fmt.Errorf("an API key id needs an equals sign: write `--%s=%s`",
					apiKeyFlag.Name, args[0])
			}
			return fmt.Errorf("%s takes no arguments such as %q; everything comes from flags and the environment", cmd.CommandPath(), args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			ctx := cmd.Context()
			opts := internal.ResolveClientOptions()
			opts.SettingSources = slices.Concat(profileSources, opts.SettingSources)
			promptedFrom := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			var authWith credential.Name
			switch {
			case cmd.Flags().Changed(apiKeyFlag.Name.String()):
				authWith = credential.ApiKeyName
				opts.SettingSources = append(opts.SettingSources,
					apiKeyFlag.AsSourceUnless(func(id uuid.UUID) bool {
						return id == uuid.Nil()
					}),
					newPromptingSource(setting.ApiKeyClientSecret.EnvKey(), &openStdinFlag, promptedFrom, "API Client Secret"),
				)
				// An API key login needs no workspace, so it asks for one only while stdin carries no secret.
				if !openStdinFlag.Value {
					opts.SettingSources = append(opts.SettingSources, newWorkspaceSelectionSource(promptedFrom, true))
				}
			case apiTokenFlag.Value:
				authWith = credential.ManualName
				opts.SettingSources = append(opts.SettingSources, newPromptingSource(setting.ApiToken.EnvKey(), &openStdinFlag, promptedFrom, "API Token"))
			default:
				if openStdinFlag.Value {
					return fmt.Errorf("--%s reads the secret of --%s or --%s, and a browser login has none",
						openStdinFlag.Name, apiKeyFlag.Name, apiTokenFlag.Name)
				}
				authWith = credential.OidcLoginName
				opts.SettingSources = append(opts.SettingSources, newWorkspaceSelectionSource(promptedFrom, false))
			}

			opts.SettingSources = append(opts.SettingSources, newProfileSelectionSource(promptedFrom, openStdinFlag.Value))

			session, storeSession, unlock, err := auth.Login(ctx, authWith, opts)
			if errors.Is(err, profile.ErrInUse) {
				// Only the cause, as the setting a lookup failed for adds nothing to do about it.
				return fmt.Errorf("cannot log in while %w, such as meshstack profile or another login; quit it and try again", profile.ErrInUse)
			}
			if err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, unlock())
			}()
			_, err = session.GetBearerToken(ctx)
			if err != nil {
				return err
			}
			if _, err := session.MeshInfo(); err != nil {
				return err
			}
			if err := storeSession(ctx); err != nil {
				return err
			}
			return showStatus(cmd, output, session)
		},
	}

	cmd.MarkFlagsMutuallyExclusive(
		apiKeyFlag.Register(cmd.Flags()),
		apiTokenFlag.Register(cmd.Flags()),
	)
	apiKeyOption := cmd.Flags().Lookup(apiKeyFlag.Name.String())
	// NoOptDefVal is what makes a bare --apikey, with no value after it, parse.
	apiKeyOption.NoOptDefVal = apiKeyIdFromEnvironment
	apiKeyOption.Value = bareApiKeyFlag{apiKeyOption.Value}

	openStdinFlag.Register(cmd.Flags())
	output.Register(cmd.Flags())

	return cmd
}

const apiKeyIdFromEnvironment = "<id>"

type bareApiKeyFlag struct{ pflag.Value }

func (f bareApiKeyFlag) Set(id string) error {
	switch id {
	case apiKeyIdFromEnvironment:
		return nil
	case "":
		return errors.New("the API key id is empty; give a bare --apikey to read it from the environment")
	}
	return f.Value.Set(id)
}
