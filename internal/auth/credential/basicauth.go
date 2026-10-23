package credential

import (
	"github.com/meshcloud/meshstack-cli/client/http"
)

var _ Static = &BasicAuth{}

type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (basic *BasicAuth) Name() Name {
	return BasicAuthName
}

func (basic *BasicAuth) Identity() Identity {
	return identityOf(basic)
}

func (basic *BasicAuth) Authorization() http.Authorization {
	return http.BasicAuth(*basic)
}
