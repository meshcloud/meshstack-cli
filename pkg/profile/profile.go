// Package profile reads and writes the meshStack CLI's configuration on disk:
// `config.json`, which describes every profile, and `credentials/<profile>.json`,
// which holds one profile's credentials and its cached access tokens.
//
// Both front ends use it. The Terraform provider reads a profile the way the AWS
// provider reads `~/.aws`, and it has to write rotated refresh tokens back, so the
// lock a Store takes is cross-tool rather than an internal detail.
//
// The two files are separate so that a renewal locks only the profile it renews:
// `config.json` is never locked, so editing a profile never waits for a network round
// trip, and a failed credential write can never cost a user their configuration.
//
// Nothing here is created until a command actually needs to store something, so a
// process that only reads leaves no trace in the user's configuration directory.
package profile

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
)

// Version is the format version both files carry. A CLI that reads a higher one
// reports it and stops, because a file it does not understand may hold fields whose
// absence changes what a write means.
//
// Every other field on both structs is left out when its Go zero value means "not set". This
// one is tagged without `omitzero`, because both readers start from this constant: a file that
// carried no version would silently claim to be the current one.
const Version = 1

// writeOptions are shared by both files. Deterministic is what stops them churning: v2 writes
// map members in Go's randomised iteration order, so without it an hourly token renewal
// reshuffles accessTokens and the file changes when nothing in it did.
var writeOptions = json.JoinOptions(jsontext.WithIndent("  "), json.Deterministic(true))

const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
)

// Config is `config.json`: every profile this installation knows about. The map key is a
// Name, so json/v2 validates every name it reads or writes through Name's TextUnmarshaler.
type Config struct {
	Version        int              `json:"version"`
	CurrentProfile Name             `json:"currentProfile,omitzero"`
	Profiles       map[Name]Profile `json:"profiles,omitzero"`
}

// Profile is what describes a profile rather than what authenticates it. The
// credentials live in their own file, which is what keeps a renewal from locking this
// one.
type Profile struct {
	Endpoint         *xurl.URL `json:"endpoint,omitzero"`
	DefaultWorkspace string    `json:"defaultWorkspace,omitzero"`
}

// LoadConfig reads `config.json`. A missing file is an empty configuration rather than
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
		return Config{}, fmt.Errorf("config at %s could not be read", path)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, parseFailed("cannot parse the configuration", path, data, err)
	}
	if err := checkVersion(cfg.Version, path); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SaveConfig writes `config.json` atomically, creating the directory if it is missing.
// It needs no lock: only commands that configure write this file, and a renewal never
// touches it.
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	// Stamped rather than trusted: this code can only write the format it implements,
	// and reading a higher version already stopped before we got here.
	cfg.Version = Version
	data, err := json.Marshal(cfg, writeOptions)
	if err != nil {
		return fmt.Errorf("%s could not be encoded: %w", path, err)
	}
	return writeFileAtomic(path, append(data, '\n'))
}

func parseFailed(summary, path string, data []byte, cause error) error {
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("{")) {
		return fmt.Errorf("%s: %s is not JSON: %w. The meshStack CLI stored its configuration as YAML in earlier builds; run `meshstack login` to write it again", summary, path, cause)
	}
	return fmt.Errorf("%s: %s is not valid JSON: %w", summary, path, cause)
}

// checkVersion stops on a file written by a newer CLI, naming the file so the reader
// knows which one to look at.
func checkVersion(version int, path string) error {
	if version <= Version {
		return nil
	}
	return fmt.Errorf("configuration is newer than this CLI: %s is version %d, and this CLI understands version %d. Upgrade the meshStack CLI", path, version, Version)
}

// writeFileAtomic writes through a temporary file in the same directory and renames it,
// so a reader sees either the old file or the new one and never a half-written one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("%s could not be created: %w", dir, err)
	}
	// os.CreateTemp creates with 0600, which is the mode these files need anyway.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("a temporary file could not be created in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // a no-op once the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s could not be written: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s could not be written: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%s could not be replaced: %w", path, err)
	}
	return nil
}
