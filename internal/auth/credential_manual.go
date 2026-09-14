package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var ApiTokenSetting = setting.Setting[jwt.JWT]{
	Env:   "MESHSTACK_API_TOKEN",
	Short: "A meshStack access token to send as it is. Also read from MESHSTACK_API_TOKEN.",
	Long:  "A meshStack access token to send as it is, also read from `MESHSTACK_API_TOKEN`.",
	Parse: setting.ParseTextUnmarshaler[jwt.JWT],
}

func (s Session) resolveManualCredential(ctx context.Context, opts ResolveSessionOptions) (credential.Credential, error) {
	apiToken, apiTokenErr := opts.ResolveSetting(ApiTokenSetting)
	if errors.Is(apiTokenErr, setting.ErrNoSourceProvidedValue) {
		return nil, nil
	} else if apiTokenErr != nil {
		return nil, apiTokenErr
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using setting %s as manual credential", ApiTokenSetting.EnvKey()))
	return &credential.Manual{
		Endpoint: s.Endpoint,
		Token:    apiToken,
	}, nil
}
