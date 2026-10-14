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

// TestJsonLogRedactsTheToken uses a handler that encodes attributes as JSON rather than
// formatting them with %v, which is what the Terraform provider's sink does. Without MarshalText
// that sink walks loggedHeaders as the map it is and writes the bearer token out in full.
func TestJsonLogRedactsTheToken(t *testing.T) {
	var written bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&written, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	client := newTestClientWithServer(t, func(resp gohttp.ResponseWriter, _ *gohttp.Request) {
		resp.WriteHeader(gohttp.StatusOK)
		_, _ = resp.Write([]byte(`{"answer":"served"}`))
	})
	const secret = "supersecret"
	_, err := client.WithAuthorization(http.BearerToken(secret)).DoRequest[map[string]string](t.Context(), http.MethodPost, client.ServerUrl,
		http.WithJsonPayload(map[string]string{"asked": "for"}, "application/json"))
	require.NoError(t, err)

	logged := written.String()
	assert.NotContains(t, logged, secret)
	assert.Contains(t, logged, "[REDACTED]")
	assert.Contains(t, logged, `asked`, "the request body")
	assert.Contains(t, logged, `served`, "the response body")
}
