package http

import (
	"bytes"
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

func TestLoggedBody(t *testing.T) {
	render := func(body string) string {
		head := []byte(body)
		return loggedBody{head: head[:min(len(head), loggedBodyLimit+1)]}.String()
	}

	assert.Equal(t, "Bad Gateway", render("Bad Gateway"))
	assert.Equal(t, "<binary>", render(strings.Repeat("\x00\xff", 1500)))
	assert.Equal(t, "<empty>", render(""))
	assert.True(t, strings.HasSuffix(render(strings.Repeat("ä", loggedBodyLimit)), "[TRUNCATED]"),
		"a rune the cap cut in half does not make the text binary")
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

	t.Run("both bodies are logged, and the bearer token is not", func(t *testing.T) {
		captured := logs.Capture(t)
		answer.status, answer.body = gohttp.StatusOK, `{"name":"answered"}`

		_, err := UnmarshalJsonBody[map[string]string](client.DoRequest(t.Context(), MethodPost, server.Url,
			WithJsonRequest(map[string]string{"name": "asked"}, "")))

		require.NoError(t, err)
		assert.Equal(t, []string{
			fmt.Sprintf(`level=DEBUG msg=request url=%s method=POST headers="Accept=application/json\nAuthorization=[REDACTED]\nContent-Type=application/json\nUser-Agent=meshstack-cli/test"`, server.Url),
			`level=DEBUG msg="request body" body="{\"name\":\"asked\"}"`,
			`level=DEBUG msg=response status=200 headers="Content-Length=19\nContent-Type=text/plain; charset=utf-8"`,
			`level=DEBUG msg="response body" body="{\"name\":\"answered\"}"`,
		}, captured.Lines(slog.LevelDebug))
	})

	t.Run("a long body is logged truncated", func(t *testing.T) {
		captured := logs.Capture(t)
		answer.status, answer.body = gohttp.StatusNotFound, strings.Repeat("x", 2*loggedBodyLimit)

		_ = IgnoreResponse(client.DoRequest(t.Context(), MethodGet, server.Url))

		assert.Contains(t, captured.String(), answer.body[:loggedBodyLimit]+" [TRUNCATED]")
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
		assert.Contains(t, logged, `\"asked\":\"for\"`, "the request body")
		assert.Contains(t, logged, `\"answer\":\"served\"`, "the response body")
	})
}
