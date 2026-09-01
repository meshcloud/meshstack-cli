package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
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
		{key: meshstack.Endpoint.EnvKey, want: "https://api.example.com", flag: "--endpoint"},
		{key: meshstack.Workspace.EnvKey, want: "my-workspace", flag: "--workspace"},
		{key: profile.Name.EnvKey, want: "dev", flag: "--profile"},
		{key: credential.ApiKeyId.EnvKey, want: "an-id", flag: "--api-key"},
		{key: credential.ApiSecret.EnvKey, want: "a-secret", flag: "--api-secret-stdin"},
		{key: credential.ApiToken.EnvKey, want: "a-token", flag: "--api-token-stdin"},
		{key: tty.NoInput.EnvKey, want: "true", flag: "--no-input"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			value, ok := in.Lookup(test.key)
			require.True(t, ok)
			assert.Equal(t, test.want, value)
			assert.Equal(t, test.flag, in.Describe(test.key))
		})
	}
}

func TestAnUnsetFlagDoesNotSilenceTheSourceBelowIt(t *testing.T) {
	t.Setenv(meshstack.Endpoint.EnvKey, "https://env.example.com")
	t.Setenv(tty.NoInput.EnvKey, "true")
	in := New()

	endpoint, from, err := setting.Resolve(meshstack.Endpoint, in, setting.Environ())
	require.NoError(t, err)
	assert.Equal(t, "https://env.example.com", endpoint.String())
	assert.Equal(t, meshstack.Endpoint.EnvKey, from.Describe(meshstack.Endpoint.EnvKey))

	// A boolean is the one that could go wrong: an unset --no-input must answer nothing
	// rather than "false".
	noInput, _, err := setting.Resolve(tty.NoInput, in, setting.Environ())
	require.NoError(t, err)
	assert.True(t, noInput)
}

func TestInputAnswersNothingForASettingItDoesNotCarry(t *testing.T) {
	value, ok := New().Lookup(profile.ConfigDir.EnvKey)

	assert.False(t, ok, "neither front end offers a config-directory flag")
	assert.Empty(t, value)
}
