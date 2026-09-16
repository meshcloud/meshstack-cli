package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Directory string

var DirectorySetting = setting.Setting[Directory]{
	Env: "MESHSTACK_CONFIG_DIR",
	Short: func(envKey string) string {
		return fmt.Sprintf("The directory holding config.json and one credentials file per profile. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The directory holding the meshStack CLI's configuration, also read from `%s`.\n\n"+
			"`config.json` describes every profile, and `credentials/<profile>.json` holds that profile's "+
			"credentials and its cached tokens.", envKey)
	},
	Default: setting.DefaultSource(func() (dir string, err error) {
		// os.UserConfigDir already honors XDG_CONFIG_HOME on Linux. Naming it here is what
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
	Parse: setting.ParseText[Directory],
}

func (d Directory) Join(elems ...any) string {
	all := []string{string(d)}
	for _, elem := range elems {
		all = append(all, fmt.Sprintf("%v", elem))
	}
	return filepath.Join(all...)
}

func (d Directory) ProfilesJson() string {
	return d.Join("profiles.json")
}

func (d Directory) CredentialsJsonFor(profileName fmt.Stringer) string {
	return d.Join("credentials", fmt.Sprintf("%s.json", profileName))
}

func (d Directory) CredentialsCacheJsonFor(profileName, credentialName fmt.Stringer) string {
	return d.Join("credentials-cache", profileName, fmt.Sprintf("%s.json", credentialName))
}
