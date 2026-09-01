package http

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	gohttp "net/http"
	"slices"
	"strings"
)

// This package logs through slog's default logger, and there is no Logger seam any more. Both
// front ends install a handler on it before anything makes a request — cmd/meshstack a
// charmbracelet/log one, the Terraform provider a tflog bridge — so a second interface only
// meant that one process carried two logging conventions and the provider had to fill both.
//
// Three rules follow from the handler being installed late, in the provider's Configure, and
// from what that handler needs:
//
//   - Reach the logger through the slog package functions at the point of use, never through a
//     package-level slog.Default() captured at init. The default is still the built-in one when
//     this package is initialised.
//   - Pass the request's context, so use DebugContext rather than Debug. The provider's handler
//     reads terraform's logger out of the context, and a record that arrives without one is
//     dropped.
//   - Render an expensive attribute with fmt.Stringer and encoding.TextMarshaler, never with
//     slog.LogValuer. A handler resolves a LogValuer while handling the record, and the provider's
//     handler handles every record — its Enabled says yes to all of them, because terraform owns
//     the level. So a LogValuer here would pretty-print every request body of every terraform run,
//     including the ones TF_LOG then drops. The two interfaces below are read by the sink instead,
//     which reaches them only for a record it is about to write.

// loggedHeaders is the request's headers with the bearer token taken out. Both methods below
// produce that redacted form, because both are reached: the meshStack CLI's sink formats with %v
// and calls String, while terraform's sink encodes the fields with encoding/json — which, without
// MarshalText, would walk this map itself and write the Authorization header out in full.
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
			// Avoid printing that longish JWT Bearer token (which is also a secret)
			if k == "Authorization" {
				v = "[REDACTED]"
			}
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return strings.Join(lines, "\n")
}

// loggedBody is a request or response body, pretty-printed when something actually writes it.
// MarshalText is what carries it into terraform's JSON log; encoding/json would otherwise walk
// the struct and write {"Reader":{}}.
type loggedBody struct {
	io.Reader
}

var (
	_ fmt.Stringer           = loggedBody{}
	_ encoding.TextMarshaler = loggedBody{}
)

func (l loggedBody) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

func (l loggedBody) String() string {
	switch body := l.Reader.(type) {
	case nil:
		return "<empty>"
	case *bytes.Buffer:
		return bytesToPrettyJson(body.Bytes())
	default:
		return fmt.Sprintf("<unknown> %v", body)
	}
}

func bytesToPrettyJson(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err == nil {
		if indented, err := json.MarshalIndent(decoded, "", "  "); err == nil {
			return string(indented)
		}
	}
	return fmt.Sprintf("<string,len=%d> %s", len(data), string(data))
}
