package credential

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ Credential = &Manual{}

type Manual struct {
	Endpoint xurl.URL `json:"endpoint"`
	Token    jwt.JWT  `json:"token"`
}

func (manual *Manual) Name() Name {
	return ManualName
}

func (manual *Manual) Identity() Identity {
	return identityOf(manual)
}

func (manual *Manual) CachedToken(_ context.Context, _ getWorkspaceFunc) (jwt.JWT, bool) {
	return manual.Token, true
}

func (manual *Manual) RefreshCachedToken(_ context.Context, _ http.Client, _ getWorkspaceFunc) error {
	if !manual.Token.GetClaim(jwt.ExpiryClaim).Expired(0) {
		return nil
	}
	return fmt.Errorf("manual method cannot be refreshed; provide new with 'meshstack login --endpoint %s --apitoken [--stdin]'", manual.Endpoint)
}
