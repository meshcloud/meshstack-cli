package http

import (
	"context"
	"fmt"
	"io"
	"time"
)

var stallTimeout = 1 * time.Minute

// stallGuardedBody bounds the silence of a body, so a long answer that is still arriving is read to the end.
type stallGuardedBody struct {
	io.ReadCloser

	timer  *time.Timer
	cancel context.CancelCauseFunc
}

func newStallGuardedBody(body io.ReadCloser, cancel context.CancelCauseFunc) io.ReadCloser {
	stalled := fmt.Errorf("response body stalled: nothing received for %s", stallTimeout)
	return &stallGuardedBody{
		ReadCloser: body,
		timer:      time.AfterFunc(stallTimeout, func() { cancel(stalled) }),
		cancel:     cancel,
	}
}

func (b *stallGuardedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.timer.Reset(stallTimeout)
	return n, err
}

func (b *stallGuardedBody) Close() error {
	b.timer.Stop()
	// Closing the body before cancelling hands a fully read connection back to the pool.
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}
