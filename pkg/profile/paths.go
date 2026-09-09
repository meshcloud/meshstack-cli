package profile

import (
	"path/filepath"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

const DefaultName Name = "default"

// configDir resolves ConfigDir, which no front end offers a flag for.
func configDir() (string, error) {
	dir, _, err := setting.Resolve(ConfigDir)
	return dir, err
}

func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func CredentialsPath(name Name) (string, error) {
	if _, err := ParseName(string(name)); err != nil {
		return "", err
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials", string(name)+".json"), nil
}
