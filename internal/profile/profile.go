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

	// Name and ConfigDir are set by [Profile.init] after load or create, not by the JSON.
	Name      Name             `json:"-"`
	ConfigDir config.Directory `json:"-"`
}

func (p Profile) String() string {
	return string(p.Name)
}

// EndpointSource ranks below the environment, which is what lets ResolveSession catch an endpoint
// that does not match the profile rather than quietly using the profile's own.
func (p Profile) EndpointSource() setting.FallbackSource {
	return setting.FallbackSource{Source: setting.LookupSource{
		Description: fmt.Sprintf("endpoint in profile %s", p.Name),
		Func: func(_ context.Context) (string, error) {
			if p.Endpoint != nil {
				return p.Endpoint.String(), nil
			}
			return "", nil
		},
	}}
}

// WorkspaceSource ranks below the environment and below an interactive prompt: it is what a login
// remembered, never an override of what this run asks for.
func (p Profile) WorkspaceSource() setting.FallbackSource {
	return setting.FallbackSource{Source: setting.LookupSource{
		Description: fmt.Sprintf("default workspace in profile %s", p.Name),
		Func: func(_ context.Context) (string, error) {
			return string(p.DefaultWorkspace), nil
		},
	}}
}

//goland:noinspection GoMixedReceiverTypes
func (p *Profile) init(name Name, configDir config.Directory) {
	p.Name = name
	p.ConfigDir = configDir
}
