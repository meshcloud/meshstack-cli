package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogging pins that an INFO line carries no level, which rests on how charm's text formatter
// treats a level that has no style, and could change with an update of it.
func TestLogging(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	logged := func(t *testing.T, debug bool) string {
		t.Helper()
		stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, stderr.Close()) })
		setupLogging(stderr, debug)
		slog.DebugContext(t.Context(), "traced")
		slog.InfoContext(t.Context(), "Added profile 'dev'", "count", 1)
		slog.WarnContext(t.Context(), "careful")
		slog.ErrorContext(t.Context(), "failed")
		text, err := os.ReadFile(stderr.Name())
		require.NoError(t, err)
		return string(text)
	}

	t.Run("by default an INFO line is the bare message, and a warning keeps its level", func(t *testing.T) {
		assert.Equal(t, "Added profile 'dev' count=1\nWARN  careful\nERROR failed\n", logged(t, false))
	})

	t.Run("--debug gives every line its time and level", func(t *testing.T) {
		assert.Regexp(t, `^`+
			`\d{4}/\d\d/\d\d \d\d:\d\d:\d\d DEBUG traced\n`+
			`\d{4}/\d\d/\d\d \d\d:\d\d:\d\d INFO  Added profile 'dev' count=1\n`+
			`\d{4}/\d\d/\d\d \d\d:\d\d:\d\d WARN  careful\n`+
			`\d{4}/\d\d/\d\d \d\d:\d\d:\d\d ERROR failed\n$`, logged(t, true))
	})
}
