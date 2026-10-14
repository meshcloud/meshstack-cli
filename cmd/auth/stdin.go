package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

const stdinFlagName internal.FlagName = "stdin"

func newStdinFlag() internal.Flag[bool] {
	return internal.Flag[bool]{Name: stdinFlagName, Help: "prompt the API key secret or API token from stdin"}
}

func newPromptingSource(settingEnvKey string, openStdinFlag *internal.Flag[bool], p prompt.Prompt, what string) setting.FrontendSource {
	description := fmt.Sprintf("%s to read the %s from stdin", openStdinFlag.Name.SourceDescription(), what)
	return setting.LookupSource(settingEnvKey, description, func(ctx context.Context) (string, error) {
		if !openStdinFlag.Value {
			return "", nil
		}
		if err := p.Printf("%s (finish with Enter or Ctrl-D): ", what); err != nil {
			return "", err
		}
		answer, err := p.Next(ctx, what)
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
