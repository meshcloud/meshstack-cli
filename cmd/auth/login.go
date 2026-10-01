package auth

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func NewLogin() *cobra.Command {
	const apiKeyIdDefault = "<id>"
	var (
		openStdinFlag = newStdinFlag()
		apiKeyFlag    = internal.NewFlagForSetting[string]("apikey", setting.ApiKeyClientId)
		apiTokenFlag  = internal.NewFlagForSetting[bool]("apitoken", setting.ApiToken)
		output        internal.ShowFlag
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to meshStack",
		Long: `Log in to meshStack and store the credential in a profile, creating that profile where it
does not exist yet.

Unless --profile or MESHSTACK_PROFILE names the profile, it asks which of the stored profiles to log
in to, and offers only those for the endpoint where --endpoint or MESHSTACK_ENDPOINT gives one.
Where the input ends before an answer, it logs in to the current profile if that is one of them.

With no flag this is a browser login, and it asks which workspace to work in unless --workspace or
MESHSTACK_WORKSPACE already says. An API key login asks the same way, but goes on without a workspace
where it may not list workspaces, where the input ends before an answer, or where --stdin carries its
secret. --apitoken asks nothing.

Every question, the secret prompts of --stdin included, fails where no answer comes within a minute.

It ends with what meshstack auth status shows for the new login.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if cmd.Flags().Changed(apiKeyFlag.Name.String()) {
				return fmt.Errorf("an API key id needs an equals sign: write `--%s=%s`",
					apiKeyFlag.Name, args[0])
			}
			return fmt.Errorf("the meshstack auth login does not take any arguments such as '%q'; everything comes from flags and the environment", args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			opts := internal.ResolveClientOptions()
			promptedFrom := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			var authWith credential.Name
			switch {
			case cmd.Flags().Changed(apiKeyFlag.Name.String()):
				authWith = credential.ApiKeyName
				if apiKeyFlag.Value == "" {
					return fmt.Errorf("the API key id is empty; --%s= was given without an id; specify --%s to read from env",
						apiKeyFlag.Name, apiKeyFlag.Name)
				}
				opts.SettingSources = append(opts.SettingSources,
					apiKeyFlag.AsSourceUnless(func(value string) bool {
						return value == apiKeyIdDefault
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

			session, storeSession, err := auth.Login(ctx, authWith, opts)
			if err != nil {
				return err
			}
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
	// NoOptDefVal is what makes a bare --apikey, with no value after it, parse.
	cmd.Flags().Lookup(apiKeyFlag.Name.String()).NoOptDefVal = apiKeyIdDefault

	openStdinFlag.Register(cmd.Flags())
	output.Register(cmd.Flags())

	return cmd
}
