package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --api-secret-stdin reads exactly one line, because whatever follows it on stdin belongs to
// the command rather than to the secret.
func TestReadLineTakesOneLine(t *testing.T) {
	in := New()
	in.in = fileWith(t, "fromstdin\nthis belongs to the command\n")

	secret, err := in.ReadLine()

	require.NoError(t, err)
	assert.Equal(t, "fromstdin", secret)
}

// A pipeline built with `printf %s` supplies no trailing newline, and that is the form the
// documentation shows.
func TestReadLineTakesALastLineWithoutANewline(t *testing.T) {
	in := New()
	in.in = fileWith(t, "fromstdin")

	secret, err := in.ReadLine()

	require.NoError(t, err)
	assert.Equal(t, "fromstdin", secret)
}

func fileWith(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	return file
}
