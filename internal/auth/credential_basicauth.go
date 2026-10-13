package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var ApiUsernameSetting = setting.Setting[string]{
	Env: "MESHSTACK_API_USERNAME",
	Short: func(envKey string) string {
		return fmt.Sprintf("The username that every request sends by HTTP basic auth, together with its password. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The username that every request sends by HTTP basic auth, together with its password, also read from `%s`.", envKey)
	},
	Parse: setting.ParseText[string],
}

var ApiPasswordSetting = setting.Setting[string]{
	Env: "MESHSTACK_API_PASSWORD",
	Short: func(envKey string) string {
		return fmt.Sprintf("The password belonging to the username. Also read from %s.", envKey)
	},
	Long: func(envKey string) string {
		return fmt.Sprintf("The password belonging to the username, also read from `%s`.", envKey)
	},
	Parse: setting.ParseText[string],
}

func (s Session) resolveBasicAuthCredential(ctx context.Context, settingSources setting.Sources) (Credential, error) {
	username, password, sources, err := resolveBoth(ctx, settingSources, "basic auth", ApiUsernameSetting, ApiPasswordSetting)
	if err != nil {
		return Credential{}, err
	}
	slog.DebugContext(ctx, fmt.Sprintf("Using basic auth (username %s with %d bytes long password)", username, len(password)))
	basicAuth := CacheFor(s.CurrentProfile, &credential.BasicAuth{Username: username, Password: password})
	basicAuth.Sources = sources
	return basicAuth, nil
}
