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
	Cache    *struct {
		Token jwt.JWT `json:"token"`
	} `json:"-"`
}

func NewManual(endpoint xurl.URL, token jwt.JWT) *Manual {
	manual := &Manual{Endpoint: endpoint}
	newCache(manual)
	manual.Cache.Token = token
	return manual
}

func (manual *Manual) Name() Name {
	return ManualName
}

func (manual *Manual) Identity() Identity {
	return identityOf(manual)
}

func (manual *Manual) CachedToken(_ context.Context, _ getWorkspaceFunc) (jwt.JWT, bool) {
	if manual.Cache == nil {
		return jwt.JWT{}, false
	}
	return manual.Cache.Token, true
}

func (manual *Manual) RefreshCachedToken(_ context.Context, _ http.Client, _ getWorkspaceFunc) error {
	return fmt.Errorf("manual method cannot be refreshed; provide new with 'meshstack login --endpoint %s --apitoken [--stdin]'", manual.Endpoint)
}
