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
	Env:   "MESHSTACK_API_KEY",
	Short: "The client id of a meshStack API key, which mints tokens together with its secret. Also read from MESHSTACK_API_KEY.",
	Long:  "The client id of a meshStack API key, which mints tokens together with its secret, also read from `MESHSTACK_API_KEY`.",
	Parse: setting.ParseTextUnmarshaler[uuid.UUID],
}

var ApiKeyClientSecretSetting = setting.Setting[string]{
	Env:   "MESHSTACK_API_SECRET",
	Short: "The client secret belonging to the API key. Also read from MESHSTACK_API_SECRET.",
	Long:  "The client secret belonging to the API key, also read from `MESHSTACK_API_SECRET`.",
	Parse: setting.ParseText[string],
}

func (s Session) resolveApiKeyCredential(ctx context.Context, opts ResolveSessionOptions) (credential.Credential, error) {
	apiKeyClientId, err := opts.ResolveSetting(ApiKeyClientIdSetting)
	if errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	apiKeyClientSecret, err := opts.ResolveSetting(ApiKeyClientSecretSetting)
	if errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return nil, fmt.Errorf("when specifying credential setting %s, a accompanying secret %s is required", ApiKeyClientIdSetting.EnvKey(), ApiKeyClientSecretSetting.EnvKey())
	} else if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Using api key credentials (client id %s with %d bytes long secret)", apiKeyClientId, len(apiKeyClientSecret)))
	return &credential.ApiKey{
		Endpoint:     s.Endpoint,
		ClientId:     apiKeyClientId,
		ClientSecret: apiKeyClientSecret,
	}, nil
}
