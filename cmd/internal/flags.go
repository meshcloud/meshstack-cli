package internal

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

var (
	SkipVersionCheckFlag = NewFlagForSetting[bool]("skip-version-check", setting.SkipVersionCheck)
	EndpointFlag         = NewFlagForSetting[string]("endpoint", setting.Endpoint)
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

type FlagWithPrompt struct {
	Flag[bool]
}

func NewFlagWithPrompt(name FlagName, s setting.Setting) FlagWithPrompt {
	return FlagWithPrompt{Name: name, Help: s.Help(), SettingEnvKey: s.EnvKey()}
}
