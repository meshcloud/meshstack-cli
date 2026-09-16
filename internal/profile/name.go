package profile

import (
	"encoding"
	"fmt"
	"regexp"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Name string

var NameSetting = setting.Setting[Name]{
	Env: "MESHSTACK_PROFILE",
	Short: func(envKey string) string {
		return fmt.Sprintf("The profile whose credentials and defaults this run uses. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The profile whose credentials and defaults this run uses, also read from `%s`.\n\n"+
			"A profile is a named bundle of endpoint and credential, written by "+
			"`meshstack auth login` into the meshStack CLI's configuration directory. It supplies each of those "+
			"only where nothing above it did, so it is never an override.\n\n"+
			"With no name given, the profile is the one whose endpoint matches the endpoint in use, else the one "+
			"`meshstack profile set` last selected, else `default`.", envKey)
	},
	Default: setting.StaticDefault("default"),
	Parse:   setting.ParseTextUnmarshaler[Name],
}

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func parseName(name string) (Name, error) {
	if nameRegex.MatchString(name) {
		return Name(name), nil
	}
	return "", fmt.Errorf("a profile name must match %s", nameRegex)
}

var (
	_ encoding.TextMarshaler   = Name("")
	_ encoding.TextUnmarshaler = new(Name(""))
)

func (n Name) String() string {
	return string(n)
}

func (n Name) MarshalText() ([]byte, error) {
	if _, err := parseName(string(n)); err != nil {
		return nil, err
	}
	return []byte(n), nil
}

//goland:noinspection GoMixedReceiverTypes
func (n *Name) UnmarshalText(text []byte) (err error) {
	*n, err = parseName(string(text))
	return
}
