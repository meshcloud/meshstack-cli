package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func NewLogin() *cobra.Command {
	const apiKeyIdDefault = "<id>"
	var (
		openStdinFlag = internal.Flag[bool]{Name: "stdin", Help: "prompt the API key secret or API token from stdin"}
		apiKeyFlag    = internal.NewFlagForSetting[string]("apikey", setting.ApiKeyClientId)
		apiTokenFlag  = newFlagWithPrompt("apitoken", setting.ApiToken)
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
				return fmt.Errorf("an API key id needs an equals sign: write `--%s=%s`",
					apiKeyFlag.Name, args[0])
			}
			return fmt.Errorf("the meshstack auth login does not take any arguments such as '%q'; everything comes from flags and the environment", args)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout := internal.DefaultTimeout
			var forceAuthWith auth.Method
			var sources []setting.ExplicitSource
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
					newPromptingSource(setting.ApiKeyClientSecret.EnvKey(), cmd, &openStdinFlag, func(_ context.Context) string {
						return "API Client Secret"
					}),
				)
			case apiTokenFlag.Value:
				forceAuthWith = auth.ManualMethod
				sources = append(sources, apiTokenFlag.AsSource(cmd, &openStdinFlag, "API Token"))
			default:
				// This is OIDC Login (by default)...
				timeout = 5 * time.Minute // ...and give the user more time to finish the Browser login flow
				forceAuthWith = auth.OidcLoginMethod
				sources = append(sources, newPromptingSource(meshstack.WorkspaceSetting.EnvKey(), cmd, &openStdinFlag, func(ctx context.Context) string {

				}))
			}
			return internal.RunWith(cmd.Context(), timeout, func(ctx context.Context) error {
				session, err := internal.ResolveSession(ctx, func(opts *auth.ResolveSessionOptions) {
					opts.UseSettingsFrom = append(opts.UseSettingsFrom, sources...)
					opts.ForceAuthWith = forceAuthWith
				})
				if err != nil {
					return err
				}
				sessionStatus, err := session.Status(ctx)
				if err != nil {
					return err
				}
				if err := session.Store(ctx); err != nil {
					return err
				}
				// TODO render Markdown output from model instead of logging?!
				slog.InfoContext(ctx, fmt.Sprintf("%s (version %s) logged in at meshStack %s at %s",
					sessionStatus.CliClientId, internal.Version, sessionStatus.Version, sessionStatus.Endpoint))
				return nil
			})
		},
	}

	cmd.MarkFlagsMutuallyExclusive(
		apiKeyFlag.Register(cmd.Flags()),
		apiTokenFlag.Register(cmd.Flags()),
	)
	// This makes a bare --apikey work, see above for dedicated flags handling
	cmd.Flags().Lookup(apiKeyFlag.Name.String()).NoOptDefVal = apiKeyIdDefault

	openStdinFlag.Register(cmd.Flags())

	return cmd
}

type FlagWithPrompt struct {
	internal.Flag[bool]
}

func newFlagWithPrompt(name internal.FlagName, s setting.Setting) FlagWithPrompt {
	return FlagWithPrompt{Name: name, Help: s.Help(), SettingEnvKey: s.EnvKey()}
}

func (flag *FlagWithPrompt) AsSource(cmd *cobra.Command, stdinFlag *internal.Flag[bool], prompt string) (source setting.ExplicitSource) {
	return newPromptingSource(flag.SettingEnvKey, cmd, stdinFlag, func(_ context.Context) string {
		return prompt
	})
}

func newPromptingSource(settingEnvKey string, cmd *cobra.Command, openStdinFlag *internal.Flag[bool], prompt func(context.Context) string) setting.ExplicitSource {
	description := fmt.Sprintf("%s to read the %s from stdin", openStdinFlag.Name.SourceDescription(), prompt)
	return setting.ExplicitLookupSource(settingEnvKey, description, func(ctx context.Context) (string, error) {
		if !openStdinFlag.Value {
			return "", nil
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s (finish with Enter or Ctrl-D): ", prompt(ctx)); err != nil {
			return "", err
		}
		text, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return "", errors.New("no non-whitespace input provided in prompt")
		}
		return trimmed, nil
	})
}
