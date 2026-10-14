package main

import (
	"bytes"
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

// TestUsageOnMisuse pins that cobra prints the usage for the root's SilenceUsage as it stands
// after the error, which showUsageOnMisuse relies on.
func TestUsageOnMisuse(t *testing.T) {
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		var stderr bytes.Buffer
		root := newRootCommand()
		root.SetArgs(args)
		root.SetErr(&stderr)
		require.Error(t, root.ExecuteContext(t.Context()))
		return stderr.String()
	}

	t.Run("a missing argument shows the usage after the error", func(t *testing.T) {
		assert.Regexp(t, `^Error: accepts 1 arg\(s\), received 0\nUsage:\n  meshstack api <path or URL> \[flags\]\n`, run(t, "api"))
	})

	t.Run("a flag without its value shows the usage", func(t *testing.T) {
		assert.Contains(t, run(t, "api-docs", "--describe"), "Usage:\n  meshstack api-docs")
	})

	t.Run("an unknown subcommand suggests the one meant, and shows the usage", func(t *testing.T) {
		assert.Regexp(t, `^Error: unknown command "lst" for "meshstack buildingblock"\n\nDid you mean this\?\n\tlist\n\nUsage:\n`, run(t, "bb", "lst"))
	})

	t.Run("an error of the command itself shows no usage", func(t *testing.T) {
		assert.NotContains(t, run(t, "profile", "delete", "--yes"), "Usage:")
	})
}
