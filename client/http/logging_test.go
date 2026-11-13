package http

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/logs"
)

var (
	//go:embed testdata/logged_body_redacted.json
	loggedBodyRedacted string
	//go:embed testdata/logged_body_redacted_at_any_depth.json
	loggedBodyRedactedAtAnyDepth string
)

func TestLoggedBody(t *testing.T) {
	render := func(body string, redacted ...jsontext.Pointer) string {
		head := []byte(body)
		return loggedBody{head: head[:min(len(head), loggedBodyLimit+1)], redacted: redacted}.String()
	}
	const secret = "s3cr3t"

	t.Run("JSON is indented, and a pointer redacts the whole value", func(t *testing.T) {
		assert.Equal(t, strings.TrimSuffix(loggedBodyRedacted, "\n"),
			render(`{"items":[{"secret":"`+secret+`","name":"a"},{"secret":{"nested":"`+secret+`"}}],"config":{"key":"`+secret+`"},"at":1234567890123456789,"empty":{}}`,
				"/items/*/secret", "/config"))
	})

	t.Run("a ** token matches at any depth", func(t *testing.T) {
		assert.Equal(t, strings.TrimSuffix(loggedBodyRedactedAtAnyDepth, "\n"),
			render(`{"plaintext":"`+secret+`","inputs":[{"value":{"plaintext":"`+secret+`","hash":"h"}}]}`, "/**/plaintext"))
	})

	t.Run("a url-encoded form redacts the field a one-token pointer names", func(t *testing.T) {
		assert.Equal(t, `grant_type=refresh_token&refresh_token="[REDACTED]"&client_id=an-id`,
			render(`grant_type=refresh_token&refresh_token=`+secret+`&client_id=an-id`, "/refresh_token"))
	})

	t.Run("plain text is shown as it came", func(t *testing.T) {
		assert.Equal(t, "Bad Gateway", render("Bad Gateway"))
	})

	t.Run("binary content shows only its length", func(t *testing.T) {
		assert.Equal(t, "<binary>", render(strings.Repeat("\x00\xff", 1500)))
	})

	t.Run("an empty body says so", func(t *testing.T) {
		assert.Equal(t, "<empty>", render(""))
	})

	t.Run("broken JSON shows what decoded before the error", func(t *testing.T) {
		assert.Equal(t, //nolint:testifylint // the rendering itself is tested
			"{\n  \"name\": \"a\"\n<invalid JSON: jsontext: unexpected EOF after offset 12>", render(`{"name":"a",`))
	})

	t.Run("a body beyond the cap ends with the marker", func(t *testing.T) {
		rendered := render(`{"token":"`+strings.Repeat(secret, loggedBodyLimit)+`"}`, "/token")
		assert.Equal(t, //nolint:testifylint // the rendering itself is tested
			"{\n  \"token\": \"[REDACTED]\" [TRUNCATED]", rendered,
			"a value the cap cut is redacted all the same")

		assert.True(t, strings.HasSuffix(render(strings.Repeat("ä", loggedBodyLimit)), "[TRUNCATED]"),
			"a rune the cap cut in half does not make the text binary")
	})
}

func TestLoggedBodyIncomplete(t *testing.T) {
	assert.Equal(t, "partial\n<incomplete: connection lost>",
		loggedBody{head: []byte("partial"), err: errors.New("connection lost")}.String())
}

func TestLoggedRequest(t *testing.T) {
	var answer struct {
		status int
		body   string
	}
	server := newTestServer(t, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		w.Header()["Date"] = nil
		w.WriteHeader(answer.status)
		_, _ = io.WriteString(w, answer.body)
	})
	const secret = "supersecret"
	client := NewClient(testUserAgent).WithAuthorization(BearerToken(secret))

	t.Run("both bodies are logged redacted", func(t *testing.T) {
		captured := logs.Capture(t)
		answer.status, answer.body = gohttp.StatusOK, `{"token":"`+secret+`","name":"answered"}`

		_, err := UnmarshalJsonBody[map[string]string](client.DoRequest(t.Context(), MethodPost, server.Url,
			WithJsonRequest(map[string]string{"secret": secret}, ""),
			WithRedactedJsonPointers("/secret", "/token")))

		require.NoError(t, err)
		assert.Equal(t, []string{
			fmt.Sprintf(`level=DEBUG msg=request url=%s method=POST headers="Accept=application/json\nAuthorization=[REDACTED]\nContent-Type=application/json\nUser-Agent=meshstack-cli/test"`, server.Url),
			`level=DEBUG msg="request body" body="{\n  \"secret\": \"[REDACTED]\"\n}"`,
			`level=DEBUG msg=response status=200 headers="Content-Length=41\nContent-Type=text/plain; charset=utf-8"`,
			`level=DEBUG msg="response body" body="{\n  \"token\": \"[REDACTED]\",\n  \"name\": \"answered\"\n}"`,
		}, captured.Lines(slog.LevelDebug))
	})

	t.Run("a long body is logged truncated", func(t *testing.T) {
		captured := logs.Capture(t)
		answer.status, answer.body = gohttp.StatusNotFound, strings.Repeat("x", 2*loggedBodyLimit)

		_ = IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url))

		assert.Contains(t, captured.String(), answer.body[:loggedBodyLimit]+" "+truncatedMarker)
	})

	// The Terraform provider's sink encodes attributes as JSON.
	t.Run("a JSON handler gets the redacted text", func(t *testing.T) {
		var written bytes.Buffer
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelDebug})))
		t.Cleanup(func() { slog.SetDefault(previous) })
		answer.status, answer.body = gohttp.StatusOK, `{"answer":"served"}`

		_, err := UnmarshalJsonBody[map[string]string](client.DoRequest(t.Context(), MethodPost, server.Url,
			WithJsonRequest(map[string]string{"asked": "for"}, "")))

		require.NoError(t, err)
		logged := written.String()
		assert.NotContains(t, logged, secret)
		assert.Contains(t, logged, "Authorization=[REDACTED]")
		assert.Contains(t, logged, `\"asked\": \"for\"`, "the request body")
		assert.Contains(t, logged, `\"answer\": \"served\"`, "the response body")
	})
}
