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
	// DevLocalCredentials is nil against every meshStack a user could reach. meshfed serves it
	// only under the `default` spring profile and only when the endpoints it is configured with
	// are loopback addresses, so it is present exactly on a developer's own stack — where the
	// values it carries are the seed data in meshfed's repository rather than secrets.
	//
	// It exists so that a tool can bootstrap itself against that stack from the endpoint alone:
	// `meshstack login --dev-local` reads it instead of an .env file somebody has to maintain.
	DevLocalCredentials *DevLocalCredentials `json:"devLocalCredentials,omitempty" tfsdk:"-"`
}

// DevLocalCredentials is the local dev stack's own credentials: every API key it is configured
// with, and every keycloak login seeded into its realm. Both maps are keyed the way that stack
// names them, so a consumer picks by name rather than by position.
type DevLocalCredentials struct {
	// Keyed by the key's configured name. Which one to use is the caller's choice, and they do
	// not hold the same rights: the one the Terraform suite runs as holds ADM_, while the one a
	// runner uses reaches building block runs alone.
	ApiKeys map[string]DevLocalApiKey `json:"apiKeys"`
	// Keyed by username.
	Users map[string]DevLocalUser `json:"users"`
}

type DevLocalApiKey struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// DevLocalUser is one seeded login. Workspaces is keyed by workspace identifier and holds the role
// the login has there; it is empty for a login that holds none, which is a real case rather than a
// broken entry — such a login authenticates and then sees nothing.
//
// Nothing configures a client from Workspaces: a seeded login discovers what it can reach exactly
// as any other user does. It is here so that a test knows which logins are supposed to see
// something, and can hold discovery to that without hardcoding another repository's seed data.
//
// The identifiers are plain strings because client/ may not import pkg/workspace — .golangci.yml's
// depguard rule for this package allows the standard library, client/ itself and internal/http,
// and nothing else.
type DevLocalUser struct {
	Password   string            `json:"password"`
	Workspaces map[string]string `json:"workspaces"`
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
	// client, so an unavailable backend blocks provider configuration for the whole minute
	// internal/http spends retrying, and then fails it. Opting out of the check has to opt out of
	// that too.
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
