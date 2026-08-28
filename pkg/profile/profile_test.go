package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// isolate points the package at a temporary directory, the way a user's XDG_CONFIG_HOME
// would, so that no test can reach the developer's real configuration.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(envXDGConfigHome, dir)
	t.Setenv(envConfigFile, "")
	t.Setenv(envCredentialsDir, "")
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
	require.Equal(t, filepath.Join(dir, "meshstack", "credentials", "prod.yaml"), path)

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
		require.Equal(t, filepath.Join(dir, "meshstack", "config.yaml"), configPath)

		credentialsDir, err := CredentialsDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(dir, "meshstack", "credentials"), credentialsDir)
	})

	t.Run("the platform directory is the fallback", func(t *testing.T) {
		t.Setenv(envXDGConfigHome, "")
		t.Setenv(envConfigFile, "")
		platform, err := os.UserConfigDir()
		if err != nil {
			t.Skip("no platform configuration directory in this environment")
		}

		configPath, err := ConfigPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(platform, "meshstack", "config.yaml"), configPath)
	})

	t.Run("the file and directory overrides move both", func(t *testing.T) {
		isolate(t)
		elsewhere := t.TempDir()
		t.Setenv(envConfigFile, filepath.Join(elsewhere, "other.yaml"))
		t.Setenv(envCredentialsDir, filepath.Join(elsewhere, "secrets"))

		configPath, err := ConfigPath()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(elsewhere, "other.yaml"), configPath)

		credentialsPath, err := CredentialsPath("prod")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(elsewhere, "secrets", "prod.yaml"), credentialsPath)
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
			"default":   {Endpoint: mustUrl("https://api.dev.meshcloud.io"), DefaultWorkspace: workspace.Name("my-workspace")},
			"likvid-cf": {Endpoint: mustUrl("https://federation.demo.meshcloud.io")},
		},
	}
	require.NoError(t, SaveConfig(want))

	path := filepath.Join(dir, "meshstack", "config.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(raw)
	assert.Contains(t, text, "version: 1")
	assert.Contains(t, text, "currentProfile: default")
	assert.Contains(t, text, "endpoint: https://api.dev.meshcloud.io")
	assert.Contains(t, text, "defaultWorkspace: my-workspace")
	// The profile without a workspace must not gain an empty key.
	assert.Equal(t, 1, strings.Count(text, "defaultWorkspace"))

	got, err := LoadConfig()
	require.NoError(t, err)
	want.Version = Version
	require.Equal(t, want, got)
}

func TestConfigFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}
	dir := isolate(t)

	require.NoError(t, SaveConfig(Config{CurrentProfile: "default"}))

	info, err := os.Stat(filepath.Join(dir, "meshstack", "config.yaml"))
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
			content:  "version: 2\ncurrentProfile: default\n",
			contains: "version 2",
		},
		{
			name:     "an escaping profile name",
			content:  "version: 1\nprofiles:\n  ../escape:\n    endpoint: https://example.com\n",
			contains: "../escape",
		},
		{
			name:     "a profile name starting with a dot",
			content:  "version: 1\nprofiles:\n  .hidden:\n    endpoint: https://example.com\n",
			contains: ".hidden",
		},
		{
			name:     "broken YAML",
			content:  "version: 1\nprofiles: [\n",
			contains: "config.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t)
			path := filepath.Join(dir, "meshstack", "config.yaml")
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
	require.Equal(t, "config.yaml", entries[0].Name())
}
