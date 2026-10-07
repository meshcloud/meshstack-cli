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

	remove, err := WriteBackendOverride(dir)
	require.NoError(t, err)

	t.Run("adds the http backend", func(t *testing.T) {
		written, err := fs.ReadFile(os.DirFS(dir), BackendOverrideFile)
		require.NoError(t, err)
		assert.Equal(t, backendOverride, string(written))
	})

	t.Run("refuses to replace a file of the same name", func(t *testing.T) {
		_, err := WriteBackendOverride(dir)
		require.ErrorContains(t, err, "exists already")
		assert.FileExists(t, path, "the file that was there stays")
	})

	t.Run("is gone once removed", func(t *testing.T) {
		require.NoError(t, remove())
		assert.NoFileExists(t, path)
	})
}
