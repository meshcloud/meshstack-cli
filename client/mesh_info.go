package client

import (
	"context"
	"fmt"
	"os"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/client/version"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// MeshFeatureFlag names an optional meshStack capability. /mesh/info reports each one as a
// boolean of its own, so a consumer that wants a list rather than a set of booleans — the
// Terraform provider's meshstack_instance data source does — maps them onto these names itself.
type MeshFeatureFlag string

var (
	MeshFeatureFlags                    = enum.Enum[MeshFeatureFlag]{}
	MeshFeatureFlagFourEyesRoleApproval = MeshFeatureFlags.Entry("four_eyes_role_approval")
)

// MeshInfo is the public, unauthenticated /mesh/info document, as the endpoint returns it. It
// describes the meshStack instance the client is configured against.
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
	httpClient internal.HttpClient
}

func newMeshInfoClient(httpClient internal.HttpClient) meshInfoClient {
	return meshInfoClient{httpClient: httpClient}
}

func (c meshInfoClient) Read(ctx context.Context) (MeshInfo, error) {
	return c.httpClient.DoRequest[MeshInfo](ctx, "GET", c.httpClient.RootUrl.JoinPath("/mesh/info"), http.WithAccept("application/json"))
}

func (c meshInfoClient) checkMeshVersion(ctx context.Context) error {
	// Skip before the request, not just before the comparison: /mesh/info is a GET on the retrying
	// client, so an unavailable backend blocks provider configuration for the whole retry budget
	// (~4 minutes) and then fails it. Opting out of the check has to opt out of that too.
	if os.Getenv("MESHSTACK_SKIP_VERSION_CHECK") == "true" {
		return nil
	}

	info, err := c.Read(ctx)
	if err != nil {
		return err
	}
	meshVersion, err := version.Parse(info.Version)
	if err != nil {
		return fmt.Errorf("failed to parse meshStack version %q: %w", info.Version, err)
	}
	if meshVersion.Less(MinMeshStackVersion) {
		return fmt.Errorf("unsupported meshStack version: meshStack is running version %s, but this client requires version %s or higher", meshVersion, MinMeshStackVersion)
	}
	return nil
}
