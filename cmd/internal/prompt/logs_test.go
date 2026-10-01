package prompt

import (
	"bytes"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
)

func TestHoldLogs(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var logged bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})))

	flush := holdLogs(func() {})
	slog.InfoContext(t.Context(), "first", "count", 1)
	slog.With("scope", "prompt").WithGroup("group").WarnContext(t.Context(), "second", "key", "value")
	assert.Empty(t, logged.String(), "nothing is written while the logs are held")
	flush(t.Context())

	assert.Equal(t, "level=INFO msg=first count=1\n"+
		"level=WARN msg=second scope=prompt group.key=value\n", logged.String())
	slog.InfoContext(t.Context(), "third")
	assert.Contains(t, logged.String(), "msg=third", "the flush restores the handler")
}

func TestHoldLogsReportsOnlyTheFirstWarning(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var logged bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	synctest.Test(t, func(t *testing.T) {
		warnings := 0
		flush := holdLogs(func() { warnings++ })
		slog.InfoContext(t.Context(), "no warning")
		synctest.Wait()
		assert.Zero(t, warnings)

		slog.WarnContext(t.Context(), "first")
		slog.ErrorContext(t.Context(), "second")
		synctest.Wait()
		assert.Equal(t, 1, warnings)
		flush(t.Context())
		assert.Contains(t, logged.String(), "msg=second")
	})
}
