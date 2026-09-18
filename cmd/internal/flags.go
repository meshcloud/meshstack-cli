package internal

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

var (
	SkipVersionCheckFlag = NewFlagForSetting[bool]("skip-version-check", setting.SkipVersionCheck)
	EndpointFlag         = NewFlagForSetting[string]("endpoint", setting.Endpoint)
	WorkspaceFlag        = NewFlagForSetting[string]("workspace", setting.Workspace)
)

type FlagName string

func (n FlagName) SourceDescription() string {
	return fmt.Sprintf("flag --%s", n)
}

func (n FlagName) String() string {
	return string(n)
}

type Flag[T string | bool] struct {
	Name  FlagName
	Help  string
	Value T
	// SettingEnvKey is required to use the Flag as a setting.FrontendSource.
	SettingEnvKey string
}

func NewFlagForSetting[T string | bool](name FlagName, s setting.Setting) Flag[T] {
	return Flag[T]{Name: name, Help: s.Help(), SettingEnvKey: s.EnvKey()}
}

func (flag *Flag[T]) Register(flags *pflag.FlagSet) (flagName string) {
	switch v := any(&flag.Value).(type) {
	case *string:
		flags.StringVar(v, flag.Name.String(), *v, flag.Help)
	case *bool:
		flags.BoolVar(v, flag.Name.String(), *v, flag.Help)
	default:
		panic(fmt.Sprintf("cannot register flag with value type %T", flag.Value))
	}
	return flag.Name.String()
}

func (flag *Flag[T]) AsSource() setting.FrontendSource {
	return setting.LookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func(_ context.Context) (string, error) {
		return fmt.Sprintf("%v", flag.Value), nil
	})
}

// AsSourceUnless contributes nothing but its own name while the flag still carries the
// placeholder. The source is registered anyway, so setting resolution can name it in its error.
func (flag *Flag[T]) AsSourceUnless(predicate func(T) bool) setting.FrontendSource {
	return setting.LookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func(_ context.Context) (string, error) {
		if predicate(flag.Value) {
			return "", nil
		}
		return fmt.Sprintf("%v", flag.Value), nil
	})
}
