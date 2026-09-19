package http

import (
	"bytes"
	"encoding"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"maps"
	gohttp "net/http"
	"regexp"
	"slices"
	"strings"
)

// This package logs through slog's default logger, and each front end installs its handler on it
// late: the Terraform provider does so in Configure. Two rules follow.
//
//   - Reach the logger through the slog package functions at the point of use, and pass the
//     request's context — DebugContext, not Debug. The provider's handler reads terraform's
//     logger out of the context and drops a record that arrives without one.
//   - Render an expensive attribute with fmt.Stringer and encoding.TextMarshaler, never with
//     slog.LogValuer. The provider's handler resolves a LogValuer for every record, because its
//     Enabled says yes to all of them and terraform owns the level, so a LogValuer would
//     pretty-print every request body of every run including the ones TF_LOG then drops.

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
			// Avoid printing that longish JWT Bearer token (which is also a secret)
			if k == "Authorization" {
				v = "[REDACTED]"
			}
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return strings.Join(lines, "\n")
}

// loggedBody is a request or response body, pretty-printed when a sink writes it. Without
// MarshalText terraform's JSON log would show it as {"Reader":{}}.
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

// secretNamePattern matches the name of a JSON member or of a url-encoded field whose value is a
// credential: /api/login sends clientSecret, an OIDC grant sends and returns refresh_token and
// access_token, and a meshObject carries an API key secret or a secret input. Redacting a name
// such as token_type along with them costs a debug log nothing, while missing one writes a
// reusable credential into it.
const secretNamePattern = `(?i:secret|token|password|plaintext)`

const redactedValue = "[REDACTED]"

var (
	secretName  = regexp.MustCompile(secretNamePattern)
	secretField = regexp.MustCompile(`([^&=\s]*` + secretNamePattern + `[^&=\s]*)=[^&\s]*`)
)

func bytesToPrettyJson(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}
	if redacted, err := redactSecrets(data); err == nil {
		return redacted.String()
	}
	// Not JSON: an OIDC grant sends a url-encoded form, and a gateway answers with plain text.
	return fmt.Sprintf("<string,len=%d> %s", len(data), secretField.ReplaceAllString(string(data), "${1}="+redactedValue))
}

// redactSecrets indents the body and replaces the value of every member secretName matches. It
// rewrites the token stream rather than a decoded value, so every number keeps the text it
// arrived with: decoded into float64, a large integer came back out with lost precision.
func redactSecrets(data []byte) (jsontext.Value, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	var indented bytes.Buffer
	encoder := jsontext.NewEncoder(&indented, jsontext.WithIndent("  "))
	secret := false
	for {
		if secret {
			secret = false
			if err := decoder.SkipValue(); err != nil {
				return nil, err
			}
			if err := encoder.WriteToken(jsontext.String(redactedValue)); err != nil {
				return nil, err
			}
			continue
		}
		switch decoder.PeekKind() {
		case 0:
			if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("cannot read the body as JSON: %w", err)
			}
			// An indenting encoder terminates the top-level value with a newline, which would
			// end the log line early.
			return bytes.TrimSuffix(indented.Bytes(), []byte("\n")), nil
		case '{', '}', '[', ']':
			token, err := decoder.ReadToken()
			if err != nil {
				return nil, err
			}
			if err := encoder.WriteToken(token); err != nil {
				return nil, err
			}
		default:
			container, read := decoder.StackIndex(decoder.StackDepth())
			value, err := decoder.ReadValue()
			if err != nil {
				return nil, err
			}
			secret = container == '{' && read%2 == 0 && secretName.Match(value)
			if err := encoder.WriteValue(value); err != nil {
				return nil, err
			}
		}
	}
}
