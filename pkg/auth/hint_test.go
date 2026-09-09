package auth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
)

// TestHintErrExplainsTheFailureWhoseCauseIsNotInTheErrorText holds the hint table: a 403 is
// explained by the scope the token carried. The error itself is returned untouched in every case.
func TestHintErrExplainsTheFailureWhoseCauseIsNotInTheErrorText(t *testing.T) {
	forbidden := client.HttpError{StatusCode: 403, ResponseBody: []byte("Access denied")}

	t.Run("a 403 with a workspace-scoped login names the workspace", func(t *testing.T) {
		session := &Session{Workspace: "demo", current: credential.MethodLogin}
		logs := captureLogs(t)

		// Wrapped, because what a command actually holds is the client's error inside its own.
		wrapped := fmt.Errorf("listing building blocks: %w", forbidden)
		require.Equal(t, wrapped, HintErr(wrapped, session), "the error is returned untouched")

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "scoped to workspace demo")
		assert.Contains(t, warnings[0], meshstack.Workspace.EnvKey())
	})

	t.Run("a 403 with any other credential names its own workspace", func(t *testing.T) {
		session := &Session{Workspace: "demo", current: credential.MethodApiKey}
		logs := captureLogs(t)

		require.Equal(t, forbidden, HintErr(forbidden, session))

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "this credential's own workspace is what decides")
		assert.NotContains(t, warnings[0], "demo", "an API key token is not scoped by a workspace setting")
	})

	t.Run("a 403 without a session at all", func(t *testing.T) {
		logs := captureLogs(t)

		require.Equal(t, forbidden, HintErr(forbidden, client.NewApiTokenAuthorization("pasted-token")))

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "does not reach that object")
	})

	t.Run("anything else is passed through in silence", func(t *testing.T) {
		logs := captureLogs(t)

		notFound := client.HttpError{StatusCode: 404}
		require.Equal(t, notFound, HintErr(notFound, nil))
		require.NoError(t, HintErr(nil, nil))
		require.Empty(t, logs.warnings())
	})
}

func captureLogs(t *testing.T) *logRecorder {
	t.Helper()
	recorder := &logRecorder{}
	previous := slog.Default()
	slog.SetDefault(slog.New(recorder))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return recorder
}

type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record.Clone())
	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *logRecorder) WithGroup(string) slog.Handler { return r }

func (r *logRecorder) warnings() (messages []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.records {
		if record.Level >= slog.LevelWarn {
			messages = append(messages, record.Message)
		}
	}
	return
}
