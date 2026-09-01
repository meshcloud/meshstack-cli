package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolate points the package at a temporary directory, the way a user's XDG_CONFIG_HOME
// would, so that no test can reach the developer's real configuration.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(envXDGConfigHome, dir)
	t.Setenv(envConfigDir, "")
	return dir
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"default", true},
		{"prod", true},
		{"likvid-cf", true},
		{"a.b_c-1", true},
		{"0", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"../escape", false},
		{".hidden", false},
		{".", false},
		{"..", false},
		{"a/b", false},
		{`a\b`, false},
		{"a b", false},
		{"-leading-dash", false},
		{"_leading-underscore", false},
		{"ümlaut", false},
		{"a\nb", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateName(tc.name)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestCredentialsPathRefusesEscapingName(t *testing.T) {
	dir := isolate(t)

	path, err := CredentialsPath("prod")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "meshstack", "credentials", "prod.json"), path)

	for _, name := range []string{"../escape", ".hidden", "a/b", ""} {
		_, err := CredentialsPath(name)
		require.Error(t, err, "name %q must not resolve to a path", name)
	}

	// The same refusal reaches the store, which is what a caller actually holds.
	_, err = NewFileStore("../escape")
	require.Error(t, err)
}

func TestPathsFollowTheEnvironment(t *testing.T) {
	t.Run("XDG_CONFIG_HOME wins", func(t *testing.T) {
		dir := isolate(t)

		configPath, err := ConfigPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, "meshstack", "config.json"), configPath)

		credentials, err := CredentialsPath("prod")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, "meshstack", "credentials", "prod.json"), credentials)
	})

	t.Run("the platform directory is the fallback", func(t *testing.T) {
		t.Setenv(envXDGConfigHome, "")
		t.Setenv(envConfigDir, "")
		platform, err := os.UserConfigDir()
		if err != nil {
			t.Skip("no platform configuration directory in this environment")
		}

		configPath, err := ConfigPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(platform, "meshstack", "config.json"), configPath)
	})

	t.Run("MESHSTACK_CONFIG_DIR moves both", func(t *testing.T) {
		isolate(t)
		elsewhere := t.TempDir()
		t.Setenv(envConfigDir, elsewhere)

		configPath, err := ConfigPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(elsewhere, "config.json"), configPath)

		credentialsPath, err := CredentialsPath("prod")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(elsewhere, "credentials", "prod.json"), credentialsPath)
	})
}

func TestLoadConfigMissingFileIsEmpty(t *testing.T) {
	dir := isolate(t)

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, Config{Version: Version}, cfg)

	// Reading must not create anything.
	_, err = os.Stat(filepath.Join(dir, "meshstack"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestConfigRoundTrip(t *testing.T) {
	dir := isolate(t)

	want := Config{
		CurrentProfile: "default",
		Profiles: map[string]Profile{
			"default":   {Endpoint: mustUrl("https://api.dev.meshcloud.io"), DefaultWorkspace: "my-workspace"},
			"likvid-cf": {Endpoint: mustUrl("https://federation.demo.meshcloud.io")},
		},
	}
	require.NoError(t, SaveConfig(want))

	path := filepath.Join(dir, "meshstack", "config.json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, `"version": 1`)
	assert.Contains(t, text, `"currentProfile": "default"`)
	assert.Contains(t, text, `"endpoint": "https://api.dev.meshcloud.io"`)
	assert.Contains(t, text, `"defaultWorkspace": "my-workspace"`)
	// The profile without a workspace must not gain an empty key.
	assert.Equal(t, 1, strings.Count(text, "defaultWorkspace"))

	got, err := LoadConfig()
	require.NoError(t, err)
	want.Version = Version
	require.Equal(t, want, got)
}

func TestSaveConfigWritesOnlyTheVersionOfAnEmptyConfig(t *testing.T) {
	dir := isolate(t)

	require.NoError(t, SaveConfig(Config{}))

	raw, err := os.ReadFile(filepath.Join(dir, "meshstack", "config.json"))
	require.NoError(t, err)
	require.Equal(t, "{\n  \"version\": 1\n}\n", string(raw))

	got, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, Config{Version: Version}, got)
}

func TestLoadConfigSendsAYamlFileBackToLogin(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "meshstack", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
	require.NoError(t, os.WriteFile(path, []byte("version: 1\ncurrentProfile: default\n"), fileMode))

	_, err := LoadConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "meshstack login")
}

func TestConfigFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}
	dir := isolate(t)

	require.NoError(t, SaveConfig(Config{CurrentProfile: "default"}))

	info, err := os.Stat(filepath.Join(dir, "meshstack", "config.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Join(dir, "meshstack"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

func TestLoadConfigRejects(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		contains string
	}{
		{
			name:     "a newer version",
			content:  `{"version": 2, "currentProfile": "default"}`,
			contains: "version 2",
		},
		{
			name:     "an escaping profile name",
			content:  `{"version": 1, "profiles": {"../escape": {"endpoint": "https://example.com"}}}`,
			contains: "../escape",
		},
		{
			name:     "a profile name starting with a dot",
			content:  `{"version": 1, "profiles": {".hidden": {"endpoint": "https://example.com"}}}`,
			contains: ".hidden",
		},
		{
			name:     "truncated JSON",
			content:  `{"version": 1, "profiles": [`,
			contains: "not valid JSON",
		},
		// This row is the reason the reader is encoding/json/v2 and not encoding/json. v1
		// keeps the last of two members with the same name, so a file the user got wrong
		// resolves to a value nobody wrote.
		{
			name:     "a repeated member name",
			content:  `{"version": 1, "currentProfile": "a", "currentProfile": "b"}`,
			contains: "duplicate",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t)
			path := filepath.Join(dir, "meshstack", "config.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
			require.NoError(t, os.WriteFile(path, []byte(tc.content), fileMode))

			_, err := LoadConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.contains)
			// Every message names the file it means.
			assert.Contains(t, err.Error(), path)
		})
	}
}

func TestSaveConfigRefusesInvalidName(t *testing.T) {
	isolate(t)

	err := SaveConfig(Config{Profiles: map[string]Profile{"../escape": {}}})
	require.Error(t, err)
}

func TestSaveConfigReplacesAtomically(t *testing.T) {
	dir := isolate(t)

	require.NoError(t, SaveConfig(Config{CurrentProfile: "first"}))
	require.NoError(t, SaveConfig(Config{CurrentProfile: "second"}))

	got, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "second", got.CurrentProfile)

	// No temporary file survives a successful write.
	entries, err := os.ReadDir(filepath.Join(dir, "meshstack"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "config.json", entries[0].Name())
}
