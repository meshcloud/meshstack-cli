// Package profile reads and writes the meshStack CLI's configuration on disk:
// `config.yaml`, which describes every profile, and `credentials/<profile>.yaml`,
// which holds one profile's credentials and its cached access tokens.
//
// Both front ends use it. The Terraform provider reads a profile the way the AWS
// provider reads `~/.aws`, and it has to write rotated refresh tokens back, so the
// lock a Store takes is cross-tool rather than an internal detail.
//
// The two files are separate so that a renewal locks only the profile it renews:
// `config.yaml` is never locked, so editing a profile never waits for a network round
// trip, and a failed credential write can never cost a user their configuration.
//
// Nothing here is created until a command actually needs to store something, so a
// process that only reads leaves no trace in the user's configuration directory.
package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/goccy/go-yaml"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// Version is the format version both files carry. A CLI that reads a higher one
// reports it and stops, because a file it does not understand may hold fields whose
// absence changes what a write means.
const Version = 1

// These keys are private for the same reason every other MESHSTACK_* name in this
// module is: no front end assembles a sentence out of a constant it imported, so every
// message naming one is produced in this package.
const (
	envConfigFile     = "MESHSTACK_CONFIG_FILE"
	envCredentialsDir = "MESHSTACK_CREDENTIALS_DIR"
	// os.UserConfigDir already honours XDG_CONFIG_HOME on Linux. Naming it here is what
	// makes it win on macOS and Windows too, where the platform directory differs.
	envXDGConfigHome = "XDG_CONFIG_HOME"
)

const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
)

// Config is `config.yaml`: every profile this installation knows about.
type Config struct {
	Version        int                `yaml:"version"`
	CurrentProfile string             `yaml:"currentProfile"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// Profile is what describes a profile rather than what authenticates it. The
// credentials live in their own file, which is what keeps a renewal from locking this
// one.
type Profile struct {
	Endpoint         *xurl.URL `yaml:"endpoint,omitempty"`
	DefaultWorkspace string    `yaml:"defaultWorkspace,omitempty"`
}

// nameRE bounds a profile name, which becomes a path segment under `credentials/`.
// A name containing a separator or starting with a dot could write outside the
// directory, so it is validated on read as well as on write.
var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// ValidateName reports whether name may be used as a profile name.
func ValidateName(name string) error {
	if nameRE.MatchString(name) {
		return nil
	}
	if name == "" {
		return diags.Errorf("Profile name is empty",
			"A profile name must start with a letter or a digit and may then contain letters, digits, dots, dashes and underscores, up to 64 characters.")
	}
	return diags.Errorf(fmt.Sprintf("Profile name %q is not usable", name),
		"A profile name becomes a file name under the credentials directory, so it must start with a letter or a digit and may then contain letters, digits, dots, dashes and underscores, up to 64 characters.")
}

// ConfigDir returns the directory holding `config.yaml` and `credentials/`. It is the
// platform's own configuration directory, except that XDG_CONFIG_HOME wins wherever it
// is set. The directory is not created here.
func ConfigDir() (string, error) {
	if xdg := os.Getenv(envXDGConfigHome); xdg != "" {
		return filepath.Join(xdg, "meshstack"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", diags.Wrap(err, "Cannot locate a configuration directory",
			"Set %s to a directory this process may write to.", envXDGConfigHome)
	}
	return filepath.Join(dir, "meshstack"), nil
}

// ConfigPath returns the path of `config.yaml`.
func ConfigPath() (string, error) {
	if override := os.Getenv(envConfigFile); override != "" {
		return override, nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func credentialsDir() (string, error) {
	if override := os.Getenv(envCredentialsDir); override != "" {
		return override, nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// CredentialsPath returns the path of one profile's credentials file, refusing a name
// that is not a safe path segment.
func CredentialsPath(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".yaml"), nil
}

// LoadConfig reads `config.yaml`. A missing file is an empty configuration rather than
// an error: that is the state of a fresh install.
func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Version: Version}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, diags.Wrap(err, "Cannot read the configuration",
			"%s could not be read.", path)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, diags.Wrap(err, "Cannot parse the configuration",
			"%s is not valid YAML: %v", path, err)
	}
	if err := checkVersion(cfg.Version, path); err != nil {
		return Config{}, err
	}
	// Validating here rather than at CredentialsPath is what refuses a config.yaml
	// holding a traversing name, instead of resolving it and then writing somewhere else.
	for name := range cfg.Profiles {
		if err := ValidateName(name); err != nil {
			return Config{}, diags.Errorf("Invalid profile name in the configuration",
				"%s contains the profile name %q, which cannot be used as a file name. Remove or rename it.", path, name)
		}
	}
	return cfg, nil
}

// SaveConfig writes `config.yaml` atomically, creating the directory if it is missing.
// It needs no lock: only commands that configure write this file, and a renewal never
// touches it.
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	for name := range cfg.Profiles {
		if err := ValidateName(name); err != nil {
			return err
		}
	}
	// Stamped rather than trusted: this code can only write the format it implements,
	// and reading a higher version already stopped before we got here.
	cfg.Version = Version
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return diags.Wrap(err, "Cannot write the configuration", "%s could not be encoded.", path)
	}
	return writeFileAtomic(path, data)
}

// checkVersion stops on a file written by a newer CLI, naming the file so the reader
// knows which one to look at.
func checkVersion(version int, path string) error {
	if version <= Version {
		return nil
	}
	return diags.Errorf("Configuration is newer than this CLI",
		"%s is version %d, and this CLI understands version %d. Upgrade the meshStack CLI.", path, version, Version)
}

// writeFileAtomic writes through a temporary file in the same directory and renames it,
// so a reader sees either the old file or the new one and never a half-written one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return diags.Wrap(err, "Cannot create the configuration directory", "%s could not be created.", dir)
	}
	// os.CreateTemp creates with 0600, which is the mode these files need anyway.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return diags.Wrap(err, "Cannot write the configuration", "A temporary file could not be created in %s.", dir)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return diags.Wrap(err, "Cannot write the configuration", "%s could not be written.", tmpName)
	}
	if err := tmp.Close(); err != nil {
		return diags.Wrap(err, "Cannot write the configuration", "%s could not be written.", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return diags.Wrap(err, "Cannot write the configuration", "%s could not be replaced.", path)
	}
	return nil
}
