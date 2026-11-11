package http

import (
	"bytes"
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	gohttp "net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// loggedHeaders is the request's headers with the bearer token taken out. Both methods produce
// that redacted form, because both are reached: the CLI's sink formats with %v and calls String,
// while terraform's sink encodes the fields as JSON and would otherwise walk this map itself and
// write the Authorization header out in full.
type loggedHeaders gohttp.Header

var (
	_ fmt.Stringer           = loggedHeaders(nil)
	_ encoding.TextMarshaler = loggedHeaders(nil)
)

func (l loggedHeaders) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

func (l loggedHeaders) String() string {
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(l)) {
		for _, v := range l[k] {
			if k == "Authorization" {
				v = "[REDACTED]"
			}
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return strings.Join(lines, "\n")
}

const loggedBodyLimit = 1024

var errClosedEarly = errors.New("closed before its end")

// A goroutine owns the logged bytes, because the transport may close a body while it reads it.
func newLoggedBody(ctx context.Context, message string, body io.ReadCloser) io.ReadCloser {
	if body == nil || body == gohttp.NoBody || !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return body
	}
	tap, tapped := io.Pipe()
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		head, err := io.ReadAll(io.LimitReader(tap, loggedBodyLimit+1))
		_ = tap.Close()
		slog.DebugContext(ctx, message, "body", loggedBody{head, err})
	}()
	return tappedBody{body, tapped, logged}
}

type tappedBody struct {
	io.ReadCloser

	tap    *io.PipeWriter
	logged <-chan struct{}
}

func (b tappedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.tap.Write(p[:n])
	}
	if err != nil {
		_ = b.tap.CloseWithError(err)
	}
	return n, err
}

func (b tappedBody) Close() error {
	_ = b.tap.CloseWithError(errClosedEarly)
	<-b.logged
	return b.ReadCloser.Close()
}

// Without MarshalText terraform's JSON log would show a loggedBody as {}.
type loggedBody struct {
	// head holds one byte more than loggedBodyLimit when the body was longer.
	head []byte
	err  error
}

var (
	_ fmt.Stringer           = loggedBody{}
	_ encoding.TextMarshaler = loggedBody{}
)

func (b loggedBody) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

func (b loggedBody) String() string {
	text := b.head[:min(len(b.head), loggedBodyLimit)]
	switch {
	case len(text) == 0 && b.err == nil:
		return "<empty>"
	case isBinary(text):
		return "<binary>"
	case len(b.head) > loggedBodyLimit:
		return string(text) + " [TRUNCATED]"
	case b.err != nil:
		return fmt.Sprintf("%s\n<incomplete: %v>", text, b.err)
	}
	return string(text)
}

// isBinary ignores an incomplete rune at the end, which the cap can cut.
func isBinary(data []byte) bool {
	for range utf8.UTFMax - 1 {
		if r, size := utf8.DecodeLastRune(data); r != utf8.RuneError || size != 1 {
			break
		}
		data = data[:len(data)-1]
	}
	return !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0
}
