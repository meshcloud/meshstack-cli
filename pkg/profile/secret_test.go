package profile

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr string // a phrase from the rule that has to fire
	}{
		{name: "a generated secret", secret: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO"},
		{name: "an unfamiliar shape still goes to the server", secret: "short"},
		{name: "punctuation is not our business", secret: "abc-def.ghi_jkl"},

		{name: "empty", secret: "", wantErr: "empty"},
		{name: "whitespace only", secret: "   ", wantErr: "empty"},
		{name: "a newline only", secret: "\n", wantErr: "empty"},
		{name: "a trailing newline", secret: "secret\n", wantErr: "whitespace"},
		{name: "a diagnostic line", secret: "Error: token expired", wantErr: "whitespace"},
		{name: "two lines", secret: "secret\nwarning: something", wantErr: "whitespace"},
		{name: "a tab", secret: "sec\tret", wantErr: "whitespace"},

		{name: "the api key id", secret: "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a74", wantErr: "id"},
		{name: "an uppercase uuid", secret: "6169F530-0EAA-4F7F-91B7-C4FD4AAF2A74", wantErr: "id"},
		{name: "a uuid with a non-hex digit is not one", secret: "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a7z"},
		{name: "a uuid with a dash in the wrong place is not one", secret: "6169f5300-eaa-4f7f-91b7-c4fd4aaf2a74"},
		{name: "35 characters is not a uuid", secret: "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckSecret(tc.secret)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			// The message has to say which rule fired.
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// The secret command is exercised through this test binary re-executed as a helper, so
// the test needs no shell and runs on Windows as well.
const (
	helperEnv    = "GO_MESHSTACK_SECRET_HELPER"
	helperStdout = "GO_MESHSTACK_SECRET_HELPER_STDOUT"
	helperStderr = "GO_MESHSTACK_SECRET_HELPER_STDERR"
	helperExit   = "GO_MESHSTACK_SECRET_HELPER_EXIT"
)

func TestSecretHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	// Exit before the testing framework prints its own summary, which would otherwise
	// land on the stdout the caller reads as the secret.
	defer os.Exit(0)

	if s := os.Getenv(helperStderr); s != "" {
		fmt.Fprintln(os.Stderr, s)
	}
	if s, ok := os.LookupEnv(helperStdout); ok {
		_, _ = fmt.Fprint(os.Stdout, s)
	}
	if code, err := strconv.Atoi(os.Getenv(helperExit)); err == nil && code != 0 {
		os.Exit(code)
	}
}

// helperCommand builds a clientSecretCommand that re-executes this test binary.
func helperCommand(t *testing.T, stdout, stderr string, exit int) []string {
	t.Helper()
	t.Setenv(helperEnv, "1")
	t.Setenv(helperStdout, stdout)
	t.Setenv(helperStderr, stderr)
	t.Setenv(helperExit, strconv.Itoa(exit))
	return []string{os.Args[0], "-test.run=^TestSecretHelperProcess$"}
}

func TestSecretFromStoredValue(t *testing.T) {
	m := ApiKeyMethod{ClientId: "id", ClientSecret: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO"}
	got, err := m.Secret(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO", got)
}

func TestSecretWithNeitherStoredValueNorCommand(t *testing.T) {
	m := ApiKeyMethod{ClientId: "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a74"}
	_, err := m.Secret(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a74")
}

func TestSecretFromCommand(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   string
	}{
		{name: "bare", stdout: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO", want: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO"},
		{name: "one trailing newline is stripped", stdout: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO\n", want: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO"},
		{name: "a CRLF ending is stripped", stdout: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO\r\n", want: "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: helperCommand(t, tc.stdout, "", 0)}
			got, err := m.Secret(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSecretFromCommandGoesThroughTheCheck(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantErr string
	}{
		{name: "nothing at all", stdout: "", wantErr: "empty"},
		{name: "a blank line", stdout: "\n", wantErr: "empty"},
		{name: "a diagnostic", stdout: "Error: no such secret\n", wantErr: "whitespace"},
		{name: "two lines", stdout: "secret\ntrailing noise\n", wantErr: "whitespace"},
		{name: "the api key id", stdout: "6169f530-0eaa-4f7f-91b7-c4fd4aaf2a74\n", wantErr: "id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: helperCommand(t, tc.stdout, "", 0)}
			_, err := m.Secret(t.Context())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "clientSecretCommand")
		})
	}
}

func TestSecretCommandFailureNamesTheCommandAndQuotesStderr(t *testing.T) {
	command := helperCommand(t, "", "Error: vault token expired", 3)
	m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: command}

	_, err := m.Secret(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), command[0])
	assert.Contains(t, err.Error(), "Error: vault token expired")
	assert.Contains(t, err.Error(), "exit status 3")
}

func TestSecretCommandStderrPassesThrough(t *testing.T) {
	command := helperCommand(t, "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO\n", "Warning: renewing lease", 0)
	m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: command}

	// vault's own message has to reach the user, so it is written to this process's
	// stderr rather than being swallowed.
	captured := captureStderr(t, func() {
		got, err := m.Secret(t.Context())
		require.NoError(t, err)
		require.Equal(t, "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO", got)
	})
	assert.Contains(t, captured, "Warning: renewing lease")
	// The secret itself never travels with it.
	assert.NotContains(t, captured, "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO")
}

func TestSecretCommandHonoursContext(t *testing.T) {
	command := helperCommand(t, "Ph1nBQ2rTz8kLm4WxYv6Cd0AeJsGuNiO", "", 0)
	m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: command}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := m.Secret(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clientSecretCommand")
}

func TestSecretCommandThatDoesNotExist(t *testing.T) {
	m := ApiKeyMethod{ClientId: "id", ClientSecretCommand: []string{"meshstack-no-such-secret-helper"}}
	_, err := m.Secret(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meshstack-no-such-secret-helper")
}

// captureStderr replaces the process's stderr for the duration of f.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()

	f()

	os.Stderr = original
	require.NoError(t, w.Close())
	out := <-done
	require.NoError(t, r.Close())
	return out
}
