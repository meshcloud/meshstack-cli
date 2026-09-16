package auth

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func NewLogin() *cobra.Command {
	const apiKeyIdDefault = "<id>"
	var (
		stdinFlag    bool
		apiKeyFlag   = internal.NewFlagForSetting[string]("apikey", setting.ApiKeyClientId)
		apiTokenFlag = internal.NewFlagWithPrompt("apitoken", setting.ApiToken)
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Long:  `Log in to meshStack and store the credential in a profile.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if cmd.Flags().Changed(apiKeyFlag.Name.String()) {
				return fmt.Errorf("an API key id needs an equals sign: write `--%s=%s`. --%s takes an optional value, so %q was read as a positional argument rather than as the id",
					apiKeyFlag.Name, args[0], apiKeyFlag.Name, args[0])
			}
			return fmt.Errorf("this command takes no arguments: `meshstack auth login` does not take %q. Everything it needs comes from flags and the environment", args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var sources []setting.ExplicitSource
			var forceAuthWith auth.Method
			switch {
			case cmd.Flags().Changed(apiKeyFlag.Name.String()):
				forceAuthWith = auth.ApiKeyMethod
				if apiKeyFlag.Value == "" {
					return fmt.Errorf("the API key id is empty; --%s= was given without an id; specify --%s to read from env",
						apiKeyFlag.Name, apiKeyFlag.Name)
				}
				sources = append(sources,
					// Only add apiKeyFlag as flag settikng source if we have a non-default value
					// Source is still valuable to generate a proper error hint
					apiKeyFlag.AsSourceUnless(func(value string) bool {
						return value == apiKeyIdDefault
					}),
					internal.NewPromptingSource(setting.ApiKeyClientSecret, &stdinFlag, cmd, "API Client Secret"),
				)
			case apiTokenFlag.Value:
				forceAuthWith = auth.ManualMethod
				sources = append(sources, apiTokenFlag.AsSource(cmd, &stdinFlag, "API Token"))
			default:
				// TODO pick OIDC here
				return errors.New("no credentials provided for login")
			}
			session, err := internal.ResolveSession(cmd.Context(), func(opts *auth.ResolveSessionOptions) {
				opts.UseSettingsFrom = append(opts.UseSettingsFrom, sources...)
				opts.ForceAuthWith = forceAuthWith
			})
			if err != nil {
				return err
			}
			c, err := session.Client(cmd.Context())
			if err != nil {
				return err
			}
			info, err := c.MeshInfo.Read(cmd.Context())
			if err != nil {
				return err
			}
			if err := session.Store(cmd.Context()); err != nil {
				return err
			}
			// TODO render Markdown output from model instead of logging?!
			slog.InfoContext(cmd.Context(), fmt.Sprintf("%s (version %s) logged in at meshStack %s at %s",
				info.CliClientId, internal.Version, info.Version, session.Endpoint()))
			return nil
		},
	}

	cmd.MarkFlagsMutuallyExclusive(
		apiKeyFlag.Register(cmd.Flags()),
		apiTokenFlag.Register(cmd.Flags()),
	)
	// This makes a bare --apikey work, see above for dedicated flags handling
	cmd.Flags().Lookup(apiKeyFlag.Name.String()).NoOptDefVal = apiKeyIdDefault

	cmd.Flags().BoolVar(&stdinFlag, internal.StdinFlag.Name.String(), false,
		"prompt the API key secret or API token from stdin")

	return cmd
}
