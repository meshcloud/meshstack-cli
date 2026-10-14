package logs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
)

// T is the part of testing.TB that Capture uses, so that the binary does not link package testing.
type T interface {
	Helper()
	Name() string
	Setenv(key, value string)
	Cleanup(cleanup func())
}

type Captured struct {
	buffer *buffer

	rendering sync.Mutex
	text      bytes.Buffer
}

func Capture(t T) *Captured {
	t.Helper()
	// slog's default logger is global to the process, as the environment is. Capture calls t.Setenv
	// only to have the testing package forbid t.Parallel for t and its parents.
	t.Setenv("MESHSTACK_CLI_TEST_LOGS_CAPTURED_BY", t.Name())
	c := &Captured{}
	c.buffer = install(slog.NewTextHandler(&c.text, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: withoutTime}), nil)
	t.Cleanup(c.buffer.restore)
	return c
}

func withoutTime(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return attr
}

func (c *Captured) Records(minLevel slog.Level) []slog.Record {
	var records []slog.Record
	for _, h := range c.buffer.heldAtOrAbove(minLevel) {
		records = append(records, h.record)
	}
	return records
}

func (c *Captured) Lines(minLevel slog.Level) []string {
	c.rendering.Lock()
	defer c.rendering.Unlock()
	var lines []string
	for _, h := range c.buffer.heldAtOrAbove(minLevel) {
		c.text.Reset()
		_ = h.handler.Handle(context.Background(), h.record)
		lines = append(lines, strings.TrimSuffix(c.text.String(), "\n"))
	}
	return lines
}

func (c *Captured) String() string {
	var text strings.Builder
	for _, line := range c.Lines(slog.LevelDebug) {
		text.WriteString(line + "\n")
	}
	return text.String()
}
