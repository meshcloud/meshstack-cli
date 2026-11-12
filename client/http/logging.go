package http

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	gohttp "net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	redactedMarker  = `"[REDACTED]"`
	truncatedMarker = "[TRUNCATED]"
	anyJsonField    = "*"
	anyJsonFields   = "**"
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
func newLoggedBody(ctx context.Context, message string, body io.ReadCloser, redacted redactedJsonPointers) io.ReadCloser {
	if body == nil || body == gohttp.NoBody || !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return body
	}
	tap, tapped := io.Pipe()
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		head, err := io.ReadAll(io.LimitReader(tap, loggedBodyLimit+1))
		_ = tap.Close()
		slog.DebugContext(ctx, message, "body", loggedBody{head, err, redacted})
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
	head     []byte
	err      error
	redacted redactedJsonPointers
}

var (
	_ fmt.Stringer           = loggedBody{}
	_ encoding.TextMarshaler = loggedBody{}
)

func (b loggedBody) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

func (b loggedBody) String() string {
	data := b.head[:min(len(b.head), loggedBodyLimit)]
	if len(data) == 0 && b.err == nil {
		return "<empty>"
	}
	text, jsonErr := b.redacted.pretty(data)
	if text == "" && jsonErr != nil {
		if isBinary(data) {
			return "<binary>"
		}
		// An OIDC grant sends a url-encoded form, and a gateway answers with plain text.
		text, jsonErr = string(data), nil
		if _, err := url.ParseQuery(text); err == nil {
			text = b.redacted.redactForm(text)
		}
	}
	switch {
	case len(b.head) > loggedBodyLimit:
		return text + " " + truncatedMarker
	case b.err != nil:
		return fmt.Sprintf("%s\n<incomplete: %v>", text, b.err)
	case jsonErr != nil:
		return fmt.Sprintf("%s\n<invalid JSON: %v>", text, jsonErr)
	default:
		return text
	}
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

// redactedJsonPointers are JSON Pointers (RFC 6901) in which anyJsonField matches one token and
// anyJsonFields any number of them, so /**/plaintext matches every plaintext.
type redactedJsonPointers []jsontext.Pointer

func (r redactedJsonPointers) redacts(at jsontext.Pointer) bool {
	tokens := slices.Collect(at.Tokens())
	return slices.ContainsFunc(r, func(pointer jsontext.Pointer) bool {
		return matchTokens(slices.Collect(pointer.Tokens()), tokens)
	})
}

func matchTokens(want, got []string) bool {
	switch {
	case len(want) == 0:
		return len(got) == 0
	case want[0] == anyJsonFields:
		return matchTokens(want[1:], got) || len(got) > 0 && matchTokens(want, got[1:])
	case len(got) == 0:
		return false
	default:
		return (want[0] == anyJsonField || want[0] == got[0]) && matchTokens(want[1:], got[1:])
	}
}

var formField = regexp.MustCompile(`(^|&)([^&=]*)=[^&]*`)

// redactForm reads the name of each url-encoded field as a pointer of one token, such as /refresh_token.
func (r redactedJsonPointers) redactForm(text string) string {
	return formField.ReplaceAllStringFunc(text, func(field string) string {
		parts := formField.FindStringSubmatch(field)
		name, err := url.QueryUnescape(parts[2])
		if err != nil || !r.redacts(jsontext.Pointer("").AppendToken(name)) {
			return field
		}
		return parts[1] + parts[2] + "=" + redactedMarker
	})
}

// pretty writes numbers as they arrived, so that they keep their text, and stops after the last
// complete token, so that a truncated body still shows.
func (r redactedJsonPointers) pretty(data []byte) (string, error) {
	var out strings.Builder

	newline := func(depth int) {
		out.WriteString("\n" + strings.Repeat("  ", depth))
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		depth := decoder.StackDepth()
		container, read := decoder.StackIndex(depth)
		kind := decoder.PeekKind()
		var raw []byte
		switch kind {
		case jsontext.KindInvalid:
			if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
				return out.String(), err
			}
			return out.String(), nil
		case jsontext.KindBeginObject, jsontext.KindEndObject, jsontext.KindBeginArray, jsontext.KindEndArray:
			if _, err := decoder.ReadToken(); err != nil {
				return out.String(), err
			}
			raw = []byte{byte(kind)}
		default:
			var err error
			if raw, err = decoder.ReadValue(); err != nil {
				return out.String(), err
			}
		}

		isEnd := kind == jsontext.KindEndObject || kind == jsontext.KindEndArray
		isName := container == jsontext.KindBeginObject && read%2 == 0 && !isEnd
		switch {
		case isEnd && read > 0:
			newline(depth - 1)
		case isEnd || depth == 0:
		case container == jsontext.KindBeginObject && !isName:
			out.WriteString(": ")
		default:
			if read > 0 {
				out.WriteByte(',')
			}
			newline(depth)
		}
		out.Write(raw)

		// After an object name, StackPointer points to the member's value.
		if isName && r.redacts(decoder.StackPointer()) {
			out.WriteString(": " + redactedMarker)
			if err := decoder.SkipValue(); err != nil {
				return out.String(), err
			}
		}
	}
}
