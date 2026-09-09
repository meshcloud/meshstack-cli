package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/tty"
)

// A setting is identified by its EnvKey, and this source answers every one the CLI can carry.
// Describe answers the flag, which is what makes an origin readable.
func TestInputAnswersEverySettingItsFlagsCarry(t *testing.T) {
	in := &Input{
		Endpoint:  "https://api.example.com",
		Workspace: "my-workspace",
		Profile:   "dev",
		ApiKey:    "an-id",
		ApiSecret: "a-secret",
		ApiToken:  "a-token",
		NoInput:   true,
	}

	tests := []struct {
		key  string
		want string
		flag string
	}{
		{key: meshstack.Endpoint.EnvKey(), want: "https://api.example.com", flag: "--endpoint"},
		{key: meshstack.Workspace.EnvKey(), want: "my-workspace", flag: "--workspace"},
		{key: profile.NameSetting.EnvKey(), want: "dev", flag: "--profile"},
		{key: credential.ApiKeyClientId.EnvKey(), want: "an-id", flag: "--api-key"},
		{key: credential.ApiKeyClientSecret.EnvKey(), want: "a-secret", flag: "--api-secret-stdin"},
		{key: credential.ApiBearerToken.EnvKey(), want: "a-token", flag: "--api-token-stdin"},
		{key: tty.NoInput.EnvKey(), want: "true", flag: "--no-input"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			value, err := in.Lookup(test.key)
			require.NoError(t, err)
			assert.Equal(t, test.want, value)
			assert.Equal(t, setting.SourceDescription{Type: "flag", Details: test.flag}, in.Describe(test.key))
		})
	}
}

func TestAnUnsetFlagDoesNotSilenceTheSourceBelowIt(t *testing.T) {
	t.Setenv(meshstack.Endpoint.EnvKey(), "https://env.example.com")
	t.Setenv(tty.NoInput.EnvKey(), "true")
	in := New()

	endpoint, resolution, err := setting.Resolve(meshstack.Endpoint, in.Source())
	require.NoError(t, err)
	assert.Equal(t, "https://env.example.com", endpoint.String())
	assert.Equal(t, setting.SourceDescription{Type: "environment variable", Details: meshstack.Endpoint.EnvKey()},
		resolution.From.Describe(meshstack.Endpoint.EnvKey()))

	// A boolean is the one that could go wrong: an unset --no-input must answer nothing
	// rather than "false".
	noInput, _, err := setting.Resolve(tty.NoInput, in.Source())
	require.NoError(t, err)
	assert.True(t, noInput)
}

// A source that cannot express a setting answers nothing, so that it appears in neither an
// origin nor a hint.
func TestInputAnswersNothingForASettingItDoesNotCarry(t *testing.T) {
	value, err := New().Lookup(profile.ConfigDir.EnvKey())

	require.NoError(t, err)
	assert.Empty(t, value, "neither front end offers a config-directory flag")
	assert.Empty(t, New().Describe(profile.ConfigDir.EnvKey()))
}
