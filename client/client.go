package client

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/meshcloud/meshstack-cli/client/version"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

var MinMeshStackVersion = version.MustParse("2026.35.0")

// HttpError represents an HTTP error response with status code.
// This error is returned when an HTTP request fails with a non-2XX status code.
type HttpError = http.Error

type Client struct {
	ApiKey                         MeshApiKeyClient
	BuildingBlock                  MeshBuildingBlockClient
	BuildingBlockV2                MeshBuildingBlockV2Client
	BuildingBlockRun               MeshBuildingBlockRunClient
	BuildingBlockDefinition        MeshBuildingBlockDefinitionClient
	BuildingBlockDefinitionVersion MeshBuildingBlockDefinitionVersionClient
	BuildingBlockRunner            MeshBuildingBlockRunnerClient
	Integration                    MeshIntegrationClient
	LandingZone                    MeshLandingZoneClient
	Location                       MeshLocationClient
	MeshInfo                       MeshInfoClient
	PaymentMethod                  MeshPaymentMethodClient
	Platform                       MeshPlatformClient
	PlatformType                   MeshPlatformTypeClient
	Project                        MeshProjectClient
	ProjectGroupBinding            MeshProjectGroupBindingClient
	ProjectUserBinding             MeshProjectUserBindingClient
	ServiceInstance                MeshServiceInstanceClient
	TagDefinition                  MeshTagDefinitionClient
	Tenant                         MeshTenantClient
	Workspace                      MeshWorkspaceClient
	WorkspaceGroupBinding          MeshWorkspaceGroupBindingClient
	WorkspaceUserBinding           MeshWorkspaceUserBindingClient
}

// Authorization produces the bearer token for each request, and replaces one the meshStack
// API answered with a 401. pkg/auth holds the implementation that mints, caches and persists
// tokens; see client/internal/auth.go for why that work is not in this package any more.
type Authorization = http.Authorization

// NewApiTokenAuthorization carries a token somebody else obtained. Nothing renews it, so it
// expires during long-running work; pkg/auth is what a caller with a credential wants.
func NewApiTokenAuthorization(apiToken string) Authorization {
	return http.BearerTokenAuthorization{Token: apiToken}
}

func New(ctx context.Context, rootUrl *url.URL, userAgent string, auth Authorization) (Client, error) {
	// Retries are not configured here any more. The process has one HTTP client, it carries the
	// retry policy for every caller, and which requests may be replayed is decided per request
	// rather than per client — see internal/http.
	httpClient := http.NewClient(rootUrl, userAgent, auth)

	meshInfoClient := newMeshInfoClient(httpClient)
	if err := checkMeshVersion(ctx, meshInfoClient); err != nil {
		return Client{}, err
	}

	return Client{
		ApiKey:                         newApiKeyClient(ctx, httpClient),
		BuildingBlock:                  newBuildingBlockClient(ctx, httpClient),
		BuildingBlockV2:                newBuildingBlockV2Client(ctx, httpClient),
		BuildingBlockRun:               newBuildingBlockRunClient(ctx, httpClient),
		BuildingBlockDefinition:        newBuildingBlockDefinitionClient(ctx, httpClient),
		BuildingBlockDefinitionVersion: newBuildingBlockDefinitionVersionClient(ctx, httpClient),
		BuildingBlockRunner:            newBuildingBlockRunnerClient(ctx, httpClient),
		Integration:                    newIntegrationClient(ctx, httpClient),
		LandingZone:                    newLandingZoneClient(ctx, httpClient),
		Location:                       newLocationClient(ctx, httpClient),
		MeshInfo:                       meshInfoClient,
		PaymentMethod:                  newPaymentMethodClient(ctx, httpClient),
		Platform:                       newPlatformClient(ctx, httpClient),
		PlatformType:                   newPlatformTypeClient(ctx, httpClient),
		Project:                        newProjectClient(ctx, httpClient),
		ProjectGroupBinding:            newProjectGroupBindingClient(ctx, httpClient),
		ProjectUserBinding:             newProjectUserBindingClient(ctx, httpClient),
		ServiceInstance:                newServiceInstanceClient(ctx, httpClient),
		TagDefinition:                  newTagDefinitionClient(ctx, httpClient),
		Tenant:                         newTenantClient(ctx, httpClient),
		Workspace:                      newWorkspaceClient(ctx, httpClient),
		WorkspaceGroupBinding:          newWorkspaceGroupBindingClient(ctx, httpClient),
		WorkspaceUserBinding:           newWorkspaceUserBindingClient(ctx, httpClient),
	}, nil
}

func checkMeshVersion(ctx context.Context, meshInfoClient MeshInfoClient) error {
	// Skip before the request, not just before the comparison: /mesh/info is a GET on the retrying
	// client, so an unavailable backend blocks provider configuration for the whole retry budget
	// (~4 minutes) and then fails it. Opting out of the check has to opt out of that too.
	if os.Getenv("MESHSTACK_SKIP_VERSION_CHECK") == "true" {
		return nil
	}

	info, err := meshInfoClient.Read(ctx)
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
