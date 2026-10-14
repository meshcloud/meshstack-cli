package logs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type Held struct {
	buffer     *buffer
	released   chan struct{}
	forwarding sync.WaitGroup
}

// Hold holds back every record slog's default logger gets until Release, as a line written while
// a terminal UI draws breaks its frame. After a record at WARN or above, Hold calls notify from a
// goroutine of its own: notify may block, as tea.Program.Send does until the program reads the
// message, and a record logged in the program's Update would then wait forever, because the
// program reads the next message only once Update has returned. The records logged while notify
// blocks are held all the same, and the next call counts them.
func Hold(notify func(Warned)) *Held {
	signal := make(chan struct{}, 1)
	h := &Held{buffer: install(slog.Default().Handler(), signal), released: make(chan struct{})}
	h.forwarding.Go(func() {
		for {
			select {
			case <-h.released:
				return
			case <-signal:
				h.buffer.mu.Lock()
				warned := h.buffer.warned
				h.buffer.mu.Unlock()
				notify(warned)
			}
		}
	})
	return h
}

// Release restores the logger Hold replaced, and writes every held record to it in order. Release
// waits for a notify that has not returned yet, so call it once nothing blocks notify any more,
// such as once tea.Program.Run has returned.
func (h *Held) Release(ctx context.Context) {
	close(h.released)
	h.forwarding.Wait()
	b := h.buffer
	b.restore()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.released = true
	for _, entry := range b.held {
		if err := entry.handler.Handle(ctx, entry.record); err != nil {
			slog.WarnContext(ctx, "Could not write a log record held back while the terminal UI ran", "error", err)
		}
	}
	b.held = nil
}

// Warned counts the records at WARN or above that Hold holds back, so that a terminal UI can show
// that the log has something to read once it ends.
type Warned struct {
	Warnings, Errors int
	Latest           slog.Record
}

// String ends with the latest message, so that a view can cut it to its width.
func (w Warned) String() string {
	var counted string
	switch {
	case w.Errors == 0 && w.Warnings == 0:
		return ""
	case w.Errors == 0:
		counted = countOf(w.Warnings, "warning")
	case w.Warnings == 0:
		counted = countOf(w.Errors, "error")
	default:
		counted = countOf(w.Errors, "error") + " and " + countOf(w.Warnings, "warning")
	}
	if w.Warnings+w.Errors == 1 {
		return fmt.Sprintf("%s was logged, read it once you quit: %s", counted, w.Latest.Message)
	}
	return fmt.Sprintf("%s were logged, read them once you quit. The latest: %s", counted, w.Latest.Message)
}

func countOf(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
