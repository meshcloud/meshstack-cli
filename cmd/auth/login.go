package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

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
		Long: `Log in to meshStack and store the credential in a profile.

With no flag this is a browser login, and it asks which workspace to work in unless --workspace or
MESHSTACK_WORKSPACE already says. An API key login asks the same way, while --apitoken asks nothing.`,
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
			var sources setting.Sources
			promptedFrom := newPrompt(cmd)
			switch {
			case cmd.Flags().Changed(apiKeyFlag.Name.String()):
				forceAuthWith = auth.ApiKeyMethod
				if apiKeyFlag.Value == "" {
					return fmt.Errorf("the API key id is empty; --%s= was given without an id; specify --%s to read from env",
						apiKeyFlag.Name, apiKeyFlag.Name)
				}
				sources = append(sources,
					apiKeyFlag.AsSourceUnless(func(value string) bool {
						return value == apiKeyIdDefault
					}),
					newPromptingSource(setting.ApiKeyClientSecret.EnvKey(), &openStdinFlag, promptedFrom, "API Client Secret"),
				)
			case apiTokenFlag.Value:
				forceAuthWith = auth.ManualMethod
				sources = append(sources, apiTokenFlag.AsSource(&openStdinFlag, promptedFrom, "API Token"))
			default:
				timeout = 5 * time.Minute // a browser login waits for the person to finish it
				forceAuthWith = auth.OidcLoginMethod
			}

			// A token given with --apitoken already names the workspace it belongs to, and a
			// building block runner's token — the usual reason to pass one — may list none at all.
			if forceAuthWith != auth.ManualMethod {
				sources = append(sources, newWorkspaceSelectionSource(promptedFrom))
			}

			return internal.RunWith(cmd.Context(), timeout, func(ctx context.Context) error {
				session, err := internal.ResolveSession(ctx, func(opts *auth.ResolveSessionOptions) {
					opts.SettingSources = append(opts.SettingSources, sources...)
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
	// NoOptDefVal is what makes a bare --apikey, with no value after it, parse.
	cmd.Flags().Lookup(apiKeyFlag.Name.String()).NoOptDefVal = apiKeyIdDefault

	openStdinFlag.Register(cmd.Flags())

	return cmd
}

// newWorkspaceSelectionSource lets the person pick one of the workspaces this login can reach. It
// is a fallback source, so --workspace and MESHSTACK_WORKSPACE are taken as given, while the
// profile's default ranks below it: a login is what changes that default. It needs no --stdin,
// unlike the secret prompts above, because the list has to be shown for the choice to make sense.
func newWorkspaceSelectionSource(prompt Prompt) setting.FallbackSource {
	return setting.FallbackLookupSource(setting.Workspace.EnvKey(), "the workspace selection of this login",
		func(ctx context.Context) (string, error) {
			workspaces, err := setting.WorkspacesFromContext(ctx)
			if err != nil {
				return "", err
			}
			if single := workspaces.Single(ctx); single != nil {
				return string(single.Name()), nil
			}
			selected, err := selectWorkspace(ctx, prompt, workspaces)
			if err != nil {
				return "", err
			}
			return string(selected.Name()), nil
		})
}

// selectWorkspace asks until it gets a number. An abandoned prompt is an error rather than no
// workspace at all: a login that stored none leaves every later command without one.
func selectWorkspace(ctx context.Context, prompt Prompt, workspaces setting.Workspaces) (*setting.MeshWorkspace, error) {
	profileDefault := workspaces.ProfileDefault(ctx)
	// The printed numbers have to stay answerable, so the list is kept rather than looked up again.
	listed := make([]setting.MeshWorkspace, 0, len(workspaces.Items))
	defaultNumber := 0
	defaultQuestionMarker := ""
	for number, workspace := range workspaces.All() {
		listed = append(listed, workspace)
		defaultMarker := " "
		if profileDefault != nil && workspace.Matches(*profileDefault) {
			defaultMarker = "*"
			defaultNumber = number + 1
			defaultQuestionMarker = fmt.Sprintf(", default=%d", defaultNumber)
		}
		if err := prompt.Printf(" %s[%d] %s\n", defaultMarker, number+1, workspace); err != nil {
			return nil, err
		}
	}

	question := fmt.Sprintf("Select a workspace [1-%d%s]: ", len(listed), defaultQuestionMarker)

	for {
		if err := prompt.Printf("%s", question); err != nil {
			return nil, err
		}
		answer, err := prompt.Next(ctx, "workspace selection")
		if err != nil {
			return nil, err
		}
		// An empty answer takes the marked default, and asks again where there is none: zero is
		// out of range below.
		number, convErr := defaultNumber, error(nil)
		if answer != "" {
			number, convErr = strconv.Atoi(answer)
		}
		if convErr != nil || number < 1 || number > len(listed) {
			if err := prompt.Printf("Answer with a number between 1 and %d.\n", len(listed)); err != nil {
				return nil, err
			}
			continue
		}
		return &listed[number-1], nil
	}
}

type Prompt struct {
	in  func() <-chan string
	out io.Writer
}

func newPrompt(cmd *cobra.Command) Prompt {
	return Prompt{
		in: sync.OnceValue(func() <-chan string {
			lines := make(chan string)
			go func() {
				defer close(lines)
				answers := bufio.NewScanner(cmd.InOrStdin())
				for answers.Scan() {
					lines <- answers.Text()
				}
			}()
			return lines
		}),
		out: cmd.ErrOrStderr(),
	}
}

func (p Prompt) Next(ctx context.Context, what string) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("nothing was entered for the %s: %w", what, ctx.Err())
	case line, open := <-p.in():
		if !open {
			return "", fmt.Errorf("nothing was entered for the %s, as the prompt reached the end of its input", what)
		}
		return strings.TrimSpace(line), nil
	}
}

func (p Prompt) Printf(format string, args ...any) (err error) {
	_, err = fmt.Fprintf(p.out, format, args...)
	return
}

type FlagWithPrompt struct {
	internal.Flag[bool]
}

func newFlagWithPrompt(name internal.FlagName, s setting.Setting) FlagWithPrompt {
	return FlagWithPrompt{Flag: internal.NewFlagForSetting[bool](name, s)}
}

func (flag *FlagWithPrompt) AsSource(stdinFlag *internal.Flag[bool], prompt Prompt, what string) (source setting.FrontendSource) {
	return newPromptingSource(flag.SettingEnvKey, stdinFlag, prompt, what)
}

func newPromptingSource(settingEnvKey string, openStdinFlag *internal.Flag[bool], prompt Prompt, what string) setting.FrontendSource {
	description := fmt.Sprintf("%s to read the %s from stdin", openStdinFlag.Name.SourceDescription(), what)
	return setting.LookupSource(settingEnvKey, description, func(ctx context.Context) (string, error) {
		if !openStdinFlag.Value {
			return "", nil
		}
		if err := prompt.Printf("%s (finish with Enter or Ctrl-D): ", what); err != nil {
			return "", err
		}
		answer, err := prompt.Next(ctx, what)
		if err != nil {
			return "", err
		}
		if answer == "" {
			// An error, not an empty value: ResolveSetting reads an empty value as this source
			// having nothing and falls through to the environment, and a token exported there is
			// not what was asked for at this prompt.
			return "", errors.New("no non-whitespace input provided in prompt")
		}
		return answer, nil
	})
}
