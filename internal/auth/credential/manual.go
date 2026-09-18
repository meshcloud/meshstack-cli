package credential

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ Credential = &Manual{}

type Manual struct {
	Endpoint xurl.URL `json:"endpoint"`
	Token    jwt.JWT  `json:"token"`
}

func (manual *Manual) Identity() Identity {
	return identityOf(manual)
}

func (manual *Manual) CachedToken(_ context.Context, _ getWorkspaceFunc) (jwt.JWT, bool) {
	return manual.Token, manual.Token.String() != ""
}

func (manual *Manual) RefreshCachedToken(_ context.Context, _ http.Client, _ getWorkspaceFunc) error {
	return fmt.Errorf("manual method cannot be refreshed; provide new with 'meshstack login --endpoint %s --api-token [--stdin]'", manual.Endpoint)
}
