package http

import (
	"bytes"
	"log/slog"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJsonLogRedactsTheToken pins the redaction against a handler that encodes attributes as JSON
// rather than formatting them with %v. That is what the Terraform provider's sink does, and
// without MarshalText it walks loggedHeaders as the map it is and writes the bearer token in full
// — into a log a practitioner pastes into a bug report.
func TestJsonLogRedactsTheToken(t *testing.T) {
	var written bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
		resp.WriteHeader(gohttp.StatusOK)
		_, _ = resp.Write([]byte(`{"answer":"served"}`))
	})
	client.Authorization = BearerTokenAuthorization{Token: "supersecret"}

	_, err := DoAuthorizedRequest[map[string]string](t.Context(), client, MethodPost, client.RootUrl,
		WithPayload(map[string]string{"asked": "for"}, "application/json"))
	require.NoError(t, err)

	logged := written.String()
	assert.NotContains(t, logged, "supersecret")
	assert.Contains(t, logged, "[REDACTED]")
	// The bodies survive the JSON encoding too; a struct with no exported field would arrive
	// as an empty object and say nothing about the request that was made.
	assert.Contains(t, logged, `asked`)
	assert.Contains(t, logged, `served`)
}

// TestLogRenderingWaitsForTheSink pins that nothing formats a body until a handler writes it.
// The default logger is at info level here, so a handler that drops the debug records must never
// reach String — the pretty-printing of every request and response body is the cost this saves.
func TestLogRenderingWaitsForTheSink(t *testing.T) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rendered := 0
	body := loggedBody{&countingReader{counted: &rendered}}
	slog.Debug("request", "body", body)
	assert.Zero(t, rendered, "the dropped record still rendered its body")

	slog.Info("request", "body", body)
	assert.Equal(t, 1, rendered, "the written record did not render its body")
}

// countingReader counts how often loggedBody rendered it. It is not a *bytes.Buffer, so it takes
// the branch that prints the reader itself.
type countingReader struct {
	counted *int
}

func (c *countingReader) Read([]byte) (int, error) { return 0, nil }

func (c *countingReader) String() string {
	*c.counted++
	return "counted"
}
