package auth

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/oidc"
)

// TestHintErrExplainsTheTwoFailuresWhoseCauseIsNotInTheErrorText holds the hint table: a 403
// is explained by the scope the token carried, and a rejected refresh grant by the command
// that starts a new session. The error itself is returned untouched in every case.
func TestHintErrExplainsTheTwoFailuresWhoseCauseIsNotInTheErrorText(t *testing.T) {
	forbidden := client.HttpError{StatusCode: 403, ResponseBody: []byte("Access denied")}

	t.Run("a 403 with a workspace-scoped login names the workspace", func(t *testing.T) {
		isolate(t)
		loginProfile(t, "https://api.example.com", "demo", storedLogin())
		session := resolved(t, &fakeInput{})
		logs := captureLogs(t)

		// Wrapped, because what a command actually holds is the client's error inside its own.
		wrapped := fmt.Errorf("listing building blocks: %w", forbidden)
		require.Equal(t, wrapped, HintErr(wrapped, session), "the error is returned untouched")

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "scoped to workspace demo")
		assert.Contains(t, warnings[0], "--workspace")
	})

	t.Run("a 403 with any other credential names its own workspace", func(t *testing.T) {
		isolate(t)
		t.Setenv(envEndpoint, "https://api.example.com")
		t.Setenv(envApiKey, "key-42")
		t.Setenv(envApiSecret, testSecret)
		t.Setenv("MESHSTACK_WORKSPACE", "demo")
		session := resolved(t, &fakeInput{secret: testSecret})
		logs := captureLogs(t)

		require.Equal(t, forbidden, HintErr(forbidden, session))

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "this credential's own workspace is what decides")
		assert.NotContains(t, warnings[0], "demo", "an API key token is not scoped by --workspace")
	})

	t.Run("a 403 without a session at all", func(t *testing.T) {
		isolate(t)
		logs := captureLogs(t)

		require.Equal(t, forbidden, HintErr(forbidden, client.NewApiTokenAuthorization("pasted-token")))

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "does not reach that object")
	})

	t.Run("a rejected refresh grant names meshstack login", func(t *testing.T) {
		isolate(t)
		logs := captureLogs(t)

		err := fmt.Errorf("renewing: %w", oidc.ErrRefreshRejected)
		require.ErrorIs(t, HintErr(err, nil), oidc.ErrRefreshRejected)

		warnings := logs.warnings()
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "meshstack login")
	})

	t.Run("anything else is passed through in silence", func(t *testing.T) {
		isolate(t)
		logs := captureLogs(t)

		notFound := client.HttpError{StatusCode: 404}
		require.Equal(t, notFound, HintErr(notFound, nil))
		require.NoError(t, HintErr(nil, nil))
		require.Empty(t, logs.warnings())
	})
}
