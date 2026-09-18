package profile

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

//nolint:recvcheck // only exception is init() to set fields after unmarshalling
type Profile struct {
	Endpoint         *xurl.URL           `json:"endpoint,omitzero"`
	DefaultWorkspace meshstack.Workspace `json:"default_workspace,omitzero"`
	Credential       credential.Name     `json:"credential,omitzero"`

	// Name and ConfigDir are initialized after load/create in [Profile.init] below.
	Name      Name             `json:"-"`
	ConfigDir config.Directory `json:"-"`
}

func (p Profile) String() string {
	return string(p.Name)
}

func (p Profile) EndpointSource() setting.Source {
	return setting.LookupSource{
		Description: fmt.Sprintf("endpoint in profile %s", p.Name),
		Func: func(_ context.Context) (string, error) {
			if p.Endpoint != nil {
				return p.Endpoint.String(), nil
			}
			return "", nil
		},
	}
}

func (p Profile) WorkspaceSource() setting.Source {
	return setting.LookupSource{
		Description: fmt.Sprintf("default workspace in profile %s", p.Name),
		Func: func(_ context.Context) (string, error) {
			// Note: if Workspace is empty, resolution will silently skip this source
			return string(p.DefaultWorkspace), nil
		},
	}
}

//goland:noinspection GoMixedReceiverTypes
func (p *Profile) init(name Name, configDir config.Directory) {
	p.Name = name
	p.ConfigDir = configDir
}
