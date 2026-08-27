package http

import (
	"bytes"
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
// Two rules follow from the handler being installed late, in the provider's Configure, and
// from what that handler needs:
//
//   - Reach the logger through the slog package functions at the point of use, never through a
//     package-level slog.Default() captured at init. The default is still the built-in one when
//     this package is initialised.
//   - Pass the request's context, so use DebugContext rather than Debug. The provider's handler
//     reads terraform's logger out of the context, and a record that arrives without one is
//     dropped.

type loggedHeaders gohttp.Header

var _ fmt.Stringer = loggedHeaders(nil)

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

type loggedBody struct {
	io.Reader
}

var _ fmt.Stringer = loggedBody{}

func (l loggedBody) String() string {
	if buffer, ok := l.Reader.(*bytes.Buffer); ok {
		return bytesToPrettyJson(buffer.Bytes())
	} else if buffer == nil {
		return "<empty>"
	}
	return fmt.Sprintf("<unknown> %v", l.Reader)
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
	// should never happen as we should only transfer JSON in request/responses
	return fmt.Sprintf("<string,len=%d> %s", len(data), string(data))
}
