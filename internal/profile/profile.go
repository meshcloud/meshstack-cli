package profile

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Profile struct {
	Endpoint   *xurl.URL       `json:"endpoint,omitzero"`
	Credential credential.Name `json:"credential,omitzero"`

	// Name and Credentials are initialized after load/create in [Profile.init] below.
	Name      Name `json:"-"`
	configDir ConfigDirectory
}

func (p Profile) String() string {
	return string(p.Name)
}

func (p Profile) EndpointSource() setting.Source {
	return setting.LookupSource{
		Description: fmt.Sprintf("current profile %s", p.Name),
		Func: func() (string, error) {
			if p.Endpoint != nil {
				return p.Endpoint.String(), nil
			}
			return "", nil
		},
	}
}

//goland:noinspection GoMixedReceiverTypes
func (p *Profile) init(name Name, configDir ConfigDirectory) {
	p.Name = name
	p.configDir = configDir
}
