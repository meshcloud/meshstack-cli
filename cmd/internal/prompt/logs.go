package prompt

import (
	"context"
	"log/slog"
	"sync"
)

// WarningHeld is what RunOnTerminal sends its model once it holds back a warning, so that the
// model can tell the user that the log has something to read after the program ends.
type WarningHeld struct{}

// holdLogs holds back every log record until flush: a line written while a terminal UI draws
// breaks its view, and one written afterwards is only late.
func holdLogs(onFirstWarning func()) (flush func(ctx context.Context)) {
	held := &heldRecords{onFirstWarning: onFirstWarning}
	previous := slog.Default()
	slog.SetDefault(slog.New(heldHandler{held: held, next: previous.Handler()}))
	return func(ctx context.Context) {
		slog.SetDefault(previous)
		held.mu.Lock()
		defer held.mu.Unlock()
		for _, entry := range held.records {
			if err := entry.handler.Handle(ctx, entry.record); err != nil {
				slog.WarnContext(ctx, "Could not write a log record held back during the prompt", "error", err)
			}
		}
		held.records = nil
	}
}

type (
	heldRecords struct {
		mu             sync.Mutex
		records        []heldRecord
		onFirstWarning func()
		warned         bool
	}
	heldRecord struct {
		handler slog.Handler
		record  slog.Record
	}
	// heldHandler keeps the handler each record would have gone to, so that attributes and
	// groups added with With stay attached to their records.
	heldHandler struct {
		held *heldRecords
		next slog.Handler
	}
)

func (h heldHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h heldHandler) Handle(_ context.Context, record slog.Record) error {
	h.held.mu.Lock()
	defer h.held.mu.Unlock()
	h.held.records = append(h.held.records, heldRecord{handler: h.next, record: record.Clone()})
	if record.Level >= slog.LevelWarn && !h.held.warned {
		h.held.warned = true
		// The record can come from the model's Update. There, tea.Program.Send would block forever,
		// because the program reads the next message only after Update returns.
		go h.held.onFirstWarning()
	}
	return nil
}

func (h heldHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return heldHandler{held: h.held, next: h.next.WithAttrs(attrs)}
}

func (h heldHandler) WithGroup(name string) slog.Handler {
	return heldHandler{held: h.held, next: h.next.WithGroup(name)}
}
