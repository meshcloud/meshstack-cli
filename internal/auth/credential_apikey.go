package auth

import (
	"context"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var ApiKeyClientIdSetting = setting.Setting[uuid.UUID]{
	Env: "MESHSTACK_API_KEY",
	Short: func(envKey string) string {
		return fmt.Sprintf("The client id of a meshStack API key to authenticate with, together with its secret from %s. Also read from %s.", ApiKeyClientSecretSetting.EnvKey(), envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The client id of a meshStack API key to authenticate with, together with its secret, also read from `%s`.", envKey)
	},
	Parse: setting.ParseTextUnmarshaler[uuid.UUID],
}

var ApiKeyClientSecretSetting = setting.Setting[string]{
	Env: "MESHSTACK_API_SECRET",
	Short: func(envKey string) string {
		return fmt.Sprintf("The client secret belonging to the API key. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The client secret belonging to the API key, also read from `%s`.", envKey)
	},
	Parse: setting.ParseText[string],
}

func (s Session) resolveApiKeyCredential(ctx context.Context, settingSources setting.Sources) (Credential, error) {
	apiKeyClientId, apiKeyClientSecret, sources, err := resolveBoth(ctx, settingSources, "an API key", ApiKeyClientIdSetting, ApiKeyClientSecretSetting)
	if err != nil {
		return Credential{}, err
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using api key credentials (client id %s with %d bytes long secret)", apiKeyClientId, len(apiKeyClientSecret)))
	apiKey := CacheFor(s.CurrentProfile, &credential.ApiKey{
		Endpoint:     s.CurrentProfile.Endpoint,
		ClientId:     apiKeyClientId,
		ClientSecret: apiKeyClientSecret,
	})
	apiKey.Sources = sources
	return apiKey, nil
}
