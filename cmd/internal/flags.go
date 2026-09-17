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
	Name FlagName
	Help string
	// Bind against Value using cobra's cmd.Flags().*Var* methods.
	Value T
	// SettingEnvKey is required when using the Flag as setting.ExplicitSource
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

func (flag *Flag[T]) AsSource() setting.ExplicitSource {
	return setting.ExplicitLookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func(_ context.Context) (string, error) {
		return fmt.Sprintf("%v", flag.Value), nil
	})
}

// AsSourceUnless contributes nothing but its own name while the flag still carries placeholder.
// We still add the source so setting resolution can build a proper error hint.
func (flag *Flag[T]) AsSourceUnless(predicate func(T) bool) setting.ExplicitSource {
	return setting.ExplicitLookupSource(flag.SettingEnvKey, flag.Name.SourceDescription(), func(_ context.Context) (string, error) {
		if predicate(flag.Value) {
			return "", nil
		}
		return fmt.Sprintf("%v", flag.Value), nil
	})
}
