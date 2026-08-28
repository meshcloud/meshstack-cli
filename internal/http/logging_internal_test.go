package http

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLogRenderingWaitsForTheSink pins that nothing formats a body until a handler writes it.
// The default logger is at info level here, so a handler that drops the debug records must never
// reach String — the pretty-printing of every request and response body is the cost this saves.
//
// It stays in package http because it builds a loggedBody directly: the client only ever wraps a
// bytes.Buffer, which renders no matter who asks, so counting the renders needs a reader of its own.
func TestLogRenderingWaitsForTheSink(t *testing.T) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rendered := 0
	body := loggedBody{&countingReader{counted: &rendered}}
	slog.Debug("request", "body", body)
	assert.Zero(t, rendered, "the dropped record still rendered its body")

	slog.Info("request", "body", body)
	assert.Equal(t, 1, rendered, "the written record did not render its body")
}

// countingReader counts how often loggedBody rendered it. It is not a *bytes.Buffer, so it takes
// the branch that prints the reader itself.
type countingReader struct {
	counted *int
}

func (c *countingReader) Read([]byte) (int, error) { return 0, nil }

func (c *countingReader) String() string {
	*c.counted++
	return "counted"
}
