package client

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/version"
)

// MeshFeatureFlag names an optional meshStack capability. /mesh/info reports each one as a bool of
// its own, so a consumer that wants a list of names maps them itself.
type MeshFeatureFlag string

var (
	MeshFeatureFlags                    = enum.Enum[MeshFeatureFlag]{}
	MeshFeatureFlagFourEyesRoleApproval = MeshFeatureFlags.Entry("four_eyes_role_approval")
)

// MeshInfo is the public, unauthenticated /mesh/info document, as the endpoint returns it.
type MeshInfo struct {
	Version string `json:"version" tfsdk:"version"`
	// Is4EPEnabled means "Is four-eyes principle enabled"
	Is4EPEnabled             bool              `json:"is4EPEnabled" tfsdk:"-"`
	Metadata                 map[string]string `json:"metadata" tfsdk:"metadata"`
	AdminWorkspaceIdentifier string            `json:"adminWorkspaceIdentifier" tfsdk:"admin_workspace_identifier"`
	Issuer                   xurl.URL          `json:"issuer" tfsdk:"-"`
	CliClientId              string            `json:"cliClientId" tfsdk:"-"`
}

type MeshInfoClient interface {
	Read(ctx context.Context) (MeshInfo, error)
}

type meshInfoClient struct {
	http.Client

	Endpoint xurl.URL
}

func NewMeshInfoClient(client http.Client, endpoint xurl.URL) MeshInfoClient {
	return meshInfoClient{client, endpoint}
}

func (c meshInfoClient) Read(ctx context.Context) (MeshInfo, error) {
	return c.DoRequest[MeshInfo](ctx, "GET", c.Endpoint.JoinPath("/mesh/info"), http.WithAccept("application/json"))
}

func (info MeshInfo) CheckVersion() error {
	meshVersion, err := version.Parse(info.Version)
	if err != nil {
		return fmt.Errorf("failed to parse meshStack version %q: %w", info.Version, err)
	}
	if meshVersion.Less(MinMeshStackVersion) {
		return fmt.Errorf("unsupported meshStack version: meshStack is running version %s, but this client requires version %s or higher", meshVersion, MinMeshStackVersion)
	}
	return nil
}
