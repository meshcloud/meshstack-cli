package tfstate

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackendOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, BackendOverrideFile)
	require.NoError(t, os.WriteFile(path, []byte("# the user's own\n"), 0o600))

	_, err := WriteBackendOverride(dir)
	require.ErrorContains(t, err, "exists already")
	kept, err := fs.ReadFile(os.DirFS(dir), BackendOverrideFile)
	require.NoError(t, err)
	assert.Equal(t, "# the user's own\n", string(kept))
}

func TestModuleDir(t *testing.T) {
	for _, command := range []struct {
		args []string
		dir  string
	}{
		{[]string{"tofu", "-chdir=module", "plan"}, "module"},
		{[]string{"/usr/local/bin/terraform", "-chdir=module", "plan"}, "module"},
		{[]string{"tofu.exe", "-chdir=module", "plan"}, "module"},
		{[]string{"tofu", "-chdir=first", "-chdir=last", "plan"}, "last"},
		{[]string{"tofu", "plan", "-chdir=module"}, "."},
		{[]string{"tofu", "-chdir", "module", "plan"}, "."},
		{[]string{"tofu"}, "."},
		{[]string{"terragrunt", "-chdir=module", "plan"}, "."},
	} {
		assert.Equalf(t, command.dir, ModuleDir(command.args), "%v", command.args)
	}
}
