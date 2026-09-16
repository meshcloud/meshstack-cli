package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var ApiKeyClientIdSetting = setting.Setting[uuid.UUID]{
	Env: "MESHSTACK_API_KEY",
	Short: func(envKey string) string {
		return fmt.Sprintf("The client id of a meshStack API key, which mints tokens together with its secret. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The client id of a meshStack API key, which mints tokens together with its secret, also read from `%s`.", envKey)
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

func (s Session) resolveApiKeyCredential(ctx context.Context, opts ResolveSessionOptions) (credential.Credential, error) {
	apiKeyClientId, idErr := opts.ResolveSetting(ApiKeyClientIdSetting)
	apiKeyClientSecret, secretErr := opts.ResolveSetting(ApiKeyClientSecretSetting)
	idMissing := errors.Is(idErr, setting.ErrNoSourceProvidedValue)
	secretMissing := errors.Is(secretErr, setting.ErrNoSourceProvidedValue)
	switch {
	case idMissing && secretMissing:
		return nil, errors.Join(idErr, secretErr)
	case idMissing || secretMissing:
		// Error(), not %w: the sentinel in the chain reads as "no API key was mentioned" and is skipped.
		return nil, errors.New("an API key needs " + ApiKeyClientIdSetting.EnvKey() + " and " +
			ApiKeyClientSecretSetting.EnvKey() + " together: " + errors.Join(idErr, secretErr).Error())
	}
	if err := errors.Join(idErr, secretErr); err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using api key credentials (client id %s with %d bytes long secret)", apiKeyClientId, len(apiKeyClientSecret)))
	return &credential.ApiKey{
		Endpoint:     s.Endpoint,
		ClientId:     apiKeyClientId,
		ClientSecret: apiKeyClientSecret,
	}, nil
}
