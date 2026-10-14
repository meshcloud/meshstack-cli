package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

func TestLoginRejectsAnApiKeyIdOfNoUuidWhileItParsesTheFlags(t *testing.T) {
	configDir := withEmptyConfigDir(t)

	tests := []struct {
		arg     string
		wantErr string
	}{
		{arg: "--apikey=my-key", wantErr: `invalid argument "my-key" for "--apikey" flag: invalid uuid`},
		{arg: "--apikey=", wantErr: `invalid argument "" for "--apikey" flag: the API key id is empty; give a bare --apikey to read it from the environment`},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			_, err := execute(t, "login", tt.arg)

			require.EqualError(t, err, tt.wantErr)
			assert.NoFileExists(t, configDir.ProfilesJson())
		})
	}
}

func TestABareApiKeyFlagLeavesTheIdToTheEnvironment(t *testing.T) {
	withEmptyConfigDir(t)
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://meshstack.example.io")
	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), "my-key")
	t.Setenv(auth.ApiKeyClientSecretSetting.EnvKey(), "secret")

	_, err := execute(t, "login", "--apikey")

	assert.EqualError(t, err, "value from source 'env MESHSTACK_API_KEY' could not be parsed: invalid uuid\ntry setting flag --apikey")
}
