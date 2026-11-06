package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var ApiTokenSetting = setting.Setting[jwt.JWT]{
	Env: "MESHSTACK_API_TOKEN",
	Short: func(envKey string) string {
		return fmt.Sprintf("A meshStack access token to send as it is. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("A meshStack access token to send as it is, also read from `%s`.\n\n"+
			"The meshStack CLI does not renew this token and does not ask for a workspace, because the "+
			"token already belongs to one. A typical use is the token of a building block runner, to see "+
			"what a run sees. Such a token may not be allowed to list workspaces.", envKey)
	},
	Parse: setting.ParseTextUnmarshaler[jwt.JWT],
}

func (s Session) resolveManualCredential(ctx context.Context, settingSources setting.Sources) (Credential, error) {
	apiToken, source, apiTokenErr := settingSources.ResolveSettingWithSource(ctx, ApiTokenSetting)
	if apiTokenErr != nil {
		return Credential{}, apiTokenErr
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using setting %s as manual credential", ApiTokenSetting.EnvKey()))
	manual := CacheFor(s.CurrentProfile, credential.NewManual(s.CurrentProfile.Endpoint, apiToken))
	manual.Sources, manual.bringsItsToken = []string{source.Describe(ApiTokenSetting.EnvKey())}, true
	return manual, nil
}
