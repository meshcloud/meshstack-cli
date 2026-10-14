package json_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

func TestMarshalToKeepsTheDirectoryAndTheCredentialFileToItsOwner(t *testing.T) {
	file := filepath.Join(t.TempDir(), "created", "credentials.json")

	require.NoError(t, json.MarshalTo(t.Context(), file, map[string]string{"secret": "s3cret"}, json.UserOnlyFilePerms()))

	dir, err := os.Stat(filepath.Dir(file))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm(), "the configuration directory")

	written, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), written.Mode().Perm(), "a file holding a credential")
}
