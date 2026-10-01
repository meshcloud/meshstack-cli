package logs

import (
	"context"
	"io"
	"log" //nolint:depguard // only to undo what slog.SetDefault does to it, see restore
	"log/slog"
	"sync"
)

type buffer struct {
	previous, installed *slog.Logger
	logWriter           io.Writer
	logFlags            int
	warnedSignal        chan struct{}

	mu       sync.Mutex
	held     []held
	warned   Warned
	released bool
}

// held keeps the handler the record would have gone to, which carries the attributes and groups
// that With added.
type held struct {
	record  slog.Record
	handler slog.Handler
}

func install(next slog.Handler, warnedSignal chan struct{}) *buffer {
	b := &buffer{previous: slog.Default(), logWriter: log.Writer(), logFlags: log.Flags(), warnedSignal: warnedSignal}
	b.installed = slog.New(handler{buffer: b, next: next})
	slog.SetDefault(b.installed)
	return b
}

// restore panics where something replaced the installed logger and did not restore it, as
// restoring over it would drop one of the two. log gets its output back as well: where previous
// has slog's own default handler, which writes through log, slog.SetDefault leaves log writing to
// the installed handler, and every record would come back to it.
func (b *buffer) restore() {
	if slog.Default() != b.installed {
		panic("logs: slog's default logger was replaced while the log was held, and not restored")
	}
	slog.SetDefault(b.previous)
	log.SetOutput(b.logWriter)
	log.SetFlags(b.logFlags)
}

func (b *buffer) hold(next slog.Handler, record slog.Record) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return false
	}
	b.held = append(b.held, held{record: record.Clone(), handler: next})
	if record.Level < slog.LevelWarn {
		return true
	}
	if record.Level >= slog.LevelError {
		b.warned.Errors++
	} else {
		b.warned.Warnings++
	}
	b.warned.Latest = b.held[len(b.held)-1].record
	select {
	case b.warnedSignal <- struct{}{}:
	default:
	}
	return true
}

func (b *buffer) heldAtOrAbove(minLevel slog.Level) (records []held) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.held {
		if h.record.Level >= minLevel {
			records = append(records, h)
		}
	}
	return records
}

type handler struct {
	buffer *buffer
	next   slog.Handler
}

func (h handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle writes a record logged after Release straight through, as a logger taken from slog.Default
// earlier can outlive the hold.
func (h handler) Handle(ctx context.Context, record slog.Record) error {
	if h.buffer.hold(h.next, record) {
		return nil
	}
	return h.next.Handle(ctx, record)
}

func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return handler{buffer: h.buffer, next: h.next.WithAttrs(attrs)}
}

func (h handler) WithGroup(name string) slog.Handler {
	return handler{buffer: h.buffer, next: h.next.WithGroup(name)}
}
