package internal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func (flag *Flag[T]) AsSource() setting.ExplicitSource {
	return setting.ExplicitLookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func() (string, error) {
		return fmt.Sprintf("%v", flag.Value), nil
	})
}

// AsSourceUnless contributes nothing but its own name while the flag still carries placeholder.
// We still add the source so setting resolution can build a proper error hint.
func (flag *Flag[T]) AsSourceUnless(predicate func(T) bool) setting.ExplicitSource {
	return setting.ExplicitLookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func() (string, error) {
		if predicate(flag.Value) {
			return "", nil
		}
		return fmt.Sprintf("%v", flag.Value), nil
	})
}

func (flag *FlagWithPrompt) AsSource(cmd *cobra.Command, openStdin *bool, prompt string) (source setting.ExplicitSource) {
	return newPromptingSource(flag.SettingEnvKey, cmd, openStdin, prompt)
}

func NewPromptingSource(s setting.Setting, openStdin *bool, cmd *cobra.Command, prompt string) setting.ExplicitSource {
	return newPromptingSource(s.EnvKey(), cmd, openStdin, prompt)
}

var StdinFlag = Flag[string]{Name: "stdin"}

func newPromptingSource(settingEnvKey string, cmd *cobra.Command, openStdin *bool, prompt string) setting.ExplicitSource {
	description := fmt.Sprintf("%s to read the %s from stdin", StdinFlag.Name.SourceDescription(), prompt)
	return setting.ExplicitLookupSource(settingEnvKey, description, func() (string, error) {
		if !*openStdin {
			return "", nil
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s (finish with Enter or Ctrl-D): ", prompt); err != nil {
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
