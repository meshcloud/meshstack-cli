package credential

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/oidc/jwt"
)

var _ Credential = &OidcLogin{}

type OidcLogin struct {
	Endpoint xurl.URL `json:"endpoint"`
	Issuer   xurl.URL `json:"issuer"`
	Cache    *struct {
		RefreshToken string                      `json:"refreshToken"`
		ScopedTokens map[meshstack.Scope]jwt.JWT `json:"tokens,omitzero"`
	} `json:"-"`
}

func (oidcLogin *OidcLogin) Identity() Identity {
	return identityOf(oidcLogin)
}

func (oidcLogin *OidcLogin) CachedToken(ctx context.Context) (token jwt.JWT, found bool) {
	// TODO needs retrieval of workspace scope from ctx
	panic("implement me")
}

func (oidcLogin *OidcLogin) RefreshCachedToken(ctx context.Context, client http.Client) error {
	// TODO refresh token can also be used to exchange to a different workspace scope on demand afaik
	panic("implement me")
}
