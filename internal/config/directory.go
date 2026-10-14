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
		return fmt.Sprintf("The directory holding profiles.json and one credentials file per profile. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The directory holding the meshStack CLI's configuration, also read from `%s`.\n\n"+
			"`profiles.json` describes every profile, and `credentials/<profile>.json` holds that profile's "+
			"credentials and its cached tokens.", envKey)
	},
	Default: setting.DefaultSource(func() (dir string, err error) {
		// os.UserConfigDir already honors XDG_CONFIG_HOME on Linux. Reading it here is what makes
		// it win on macOS and Windows too, where the platform directory differs.
		if dir = os.Getenv("XDG_CONFIG_HOME"); dir == "" {
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

func (d Directory) VersionCheckJson() string {
	return d.Join("versionCheck.json")
}

func (d Directory) CredentialsJsonFor(profileName fmt.Stringer) string {
	return d.Join("credentials", fmt.Sprintf("%s.json", profileName))
}

func (d Directory) CredentialsCacheJsonFor(profileName, credentialName fmt.Stringer) string {
	return d.Join("credentials-cache", profileName, fmt.Sprintf("%s.json", credentialName))
}

func (d Directory) Exists() bool {
	if fileInfo, err := os.Stat(string(d)); err == nil && fileInfo.IsDir() {
		return true
	}
	return false
}
