package profile

import (
	"bytes"
	"encoding/json/v2"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func TestListShowsEveryProfileAndMarksTheCurrentOne(t *testing.T) {
	twoProfiles(t)

	output, err := execute(t, "", "list")

	require.NoError(t, err)
	assert.Equal(t, "| | Profile | Endpoint | Default workspace | Credential |\n"+
		"| --- | --- | --- | --- | --- |\n"+
		"| current | dev | https://a.example.io |  |  |\n"+
		"|  | prod | https://b.example.io | ops |  |\n", output)
}

func TestListWithoutProfilesSaysHowToAddOne(t *testing.T) {
	emptyConfigDir(t)

	output, err := execute(t, "", "list")

	require.NoError(t, err)
	assert.Contains(t, output, "No profiles yet.")
}

func TestListWritesJson(t *testing.T) {
	twoProfiles(t)

	output, err := execute(t, "", "list", "-o", "json")

	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &listed))
	assert.Equal(t, []map[string]any{
		{"name": "dev", "endpoint": "https://a.example.io", "current": true},
		{"name": "prod", "endpoint": "https://b.example.io", "default_workspace": "ops", "current": false},
	}, listed)
}

func TestShowWithoutATerminalTakesTheCurrentProfileWithItsStoredCredential(t *testing.T) {
	t.Setenv("MESHSTACK_ENDPOINT", "")
	profiles := twoProfiles(t)
	dev := profiles.Profiles["dev"]
	storeCredentials(t, dev)
	dev.Credential = credential.ManualName
	require.NoError(t, profiles.Store(t.Context()))

	output, err := execute(t, "", "show")

	require.NoError(t, err)
	assert.Contains(t, output, "| Profile | dev |\n")
	assert.Contains(t, output, "| Credential | API token, from file ")
	assert.Contains(t, output, "|  | No token cached yet |\n")
	assert.Contains(t, output, "This is the current profile.")
}

func TestShowWritesJsonForTheOnlyProfileOfTheEndpoint(t *testing.T) {
	twoProfiles(t)
	logs := capturedLogs(t)

	output, err := execute(t, "", "show", "--endpoint", "https://b.example.io", "-o", "json")

	require.NoError(t, err)
	var shown map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &shown))
	assert.Equal(t, "prod", shown["name"])
	assert.NotContains(t, shown, "status", "a profile without a credential has no status")
	assert.Empty(t, logs.String(), "a profile never logged in is no reason to warn")
}

func capturedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

func TestShowWithoutATerminalNeedsProfileWhereNoneOfSeveralIsCurrent(t *testing.T) {
	storedProfiles(t,
		profile.Profile{Name: "dev", Endpoint: endpointA},
		profile.Profile{Name: "prod", Endpoint: endpointB},
		profile.Profile{Name: "staging", Endpoint: endpointB},
	)

	_, err := execute(t, "", "show", "--endpoint", "https://b.example.io")

	require.ErrorContains(t, err, "name one with --profile")
}
