package http_test

import (
	"bytes"
	"log/slog"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/http"
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
	client.Authorization = http.BearerTokenAuthorization{Token: "supersecret"}

	_, err := client.DoAuthorizedRequest[map[string]string](t.Context(), http.MethodPost, client.ServerUrl,
		http.WithJsonPayload(map[string]string{"asked": "for"}, "application/json"))
	require.NoError(t, err)

	logged := written.String()
	assert.NotContains(t, logged, "supersecret")
	assert.Contains(t, logged, "[REDACTED]")
	// The bodies survive the JSON encoding too; a struct with no exported field would arrive
	// as an empty object and say nothing about the request that was made.
	assert.Contains(t, logged, `asked`)
	assert.Contains(t, logged, `served`)
}
