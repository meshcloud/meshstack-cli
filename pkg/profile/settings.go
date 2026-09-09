package profile

import (
	"encoding"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func ParseName(name string) (Name, error) {
	if nameRegex.MatchString(name) {
		return Name(name), nil
	}
	return "", fmt.Errorf("a profile name must match %s", nameRegex)
}

type Name string

func (n Name) MarshalText() ([]byte, error) {
	if _, err := ParseName(string(n)); err != nil {
		return nil, err
	} else {
		return []byte(n), nil
	}
}

//goland:noinspection GoMixedReceiverTypes
func (n *Name) UnmarshalText(text []byte) (err error) {
	*n, err = ParseName(string(text))
	return
}

var _ encoding.TextMarshaler = Name("")
var _ encoding.TextUnmarshaler = new(Name(""))

var NameSetting = setting.Setting[Name]{
	Env:   "MESHSTACK_PROFILE",
	Short: "The profile whose credentials and defaults this run uses. Also read from MESHSTACK_PROFILE.",
	Long: "The profile whose credentials and defaults this run uses, also read from `MESHSTACK_PROFILE`.\n\n" +
		"A profile is a named bundle of endpoint, credential and default workspace, written by " +
		"`meshstack auth login` into the meshStack CLI's configuration directory. It supplies each of those " +
		"only where nothing above it did, so it is never an override.\n\n" +
		"With no name given, the profile is the one whose endpoint matches the endpoint in use, else the one " +
		"`meshstack profile set` last selected, else `default`.",
	Default: setting.DefaultSource(setting.StaticLookup("default")),
	Parse:   setting.ParseTextUnmarshaler[Name],
}

var ConfigDir = setting.Setting[string]{
	Env:   "MESHSTACK_CONFIG_DIR",
	Short: "The directory holding config.json and one credentials file per profile. Also read from MESHSTACK_CONFIG_DIR.",
	Long: "The directory holding the meshStack CLI's configuration, also read from `MESHSTACK_CONFIG_DIR`.\n\n" +
		"`config.json` describes every profile, and `credentials/<profile>.json` holds that profile's " +
		"credentials and its cached tokens. The credentials directory is a convention rather than a " +
		"setting of its own, so it moves with this one and cannot be pointed elsewhere.",
	Default: setting.DefaultSource(func() (dir string, err error) {
		// os.UserConfigDir already honours XDG_CONFIG_HOME on Linux. Naming it here is what
		// makes it win on macOS and Windows too, where the platform directory differs.
		const (
			envXDGConfigHome = "XDG_CONFIG_HOME"
		)
		if dir = os.Getenv(envXDGConfigHome); dir == "" {
			if dir, err = os.UserConfigDir(); err != nil {
				return "", fmt.Errorf("cannot locate a configuration directory: %w", err)
			}
		}
		return filepath.Join(dir, "meshstack"), nil
	}),
	Parse: setting.ParseText,
}
