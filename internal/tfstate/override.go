package tfstate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// BackendOverrideFile ends in _override.tf, so tofu adds its backend to a module without one, and
// replaces the backend of a module with one. OpenTofu takes no backend type from the environment
// (opentofu/opentofu#2058). tofu reads override files in name order, so zz_ makes this one win.
const BackendOverrideFile = "zz_meshstack_override.tf"

const backendOverride = `terraform {
  backend "http" {}
}
`

// WriteBackendOverride refuses to replace a file of the same name, which may be the user's own.
func WriteBackendOverride(dir string) (remove func() error, err error) {
	path := filepath.Join(dir, BackendOverrideFile)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // G304: a name of its own, in the directory the command runs in
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%s exists already, and the override would replace it", path)
	} else if err != nil {
		return nil, err
	}
	_, err = file.WriteString(backendOverride)
	remove = func() error { return os.Remove(path) }
	if err = errors.Join(err, file.Close()); err != nil {
		return nil, errors.Join(err, remove())
	}
	return remove, nil
}
