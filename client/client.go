package client

import (
	"context"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/version"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

var MinMeshStackVersion = version.MustParse("2026.35.0")

// HttpError represents an HTTP error response with status code.
// This error is returned when an HTTP request fails with a non-2XX status code.
type HttpError = http.Error

type Client struct {
	// Endpoint is the meshStack this client was built against. It is the one thing here that is
	// not a sub-client, and it is here because nothing else keeps it: pkg/auth resolves it from a
	// block, the environment or a profile, and the sub-clients below only carry the API URLs they
	// derived from it. The Terraform provider renders it as meshstack_instance.endpoint.
	Endpoint                       string
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

// NewMeshInfoClient is a little adapter for pkg/oidc to build the oidc.Client after discovering OIDC config from meshstack instance.
func NewMeshInfoClient(ctx context.Context, rootUrl *url.URL, httpClient http.Client) MeshInfoClient {
	return newMeshInfoClient(internal.HttpClient{RootUrl: rootUrl, Client: httpClient})
}

// Authorization produces the (cached) bearer token for each request (and keeps it refreshed transparently).
type Authorization = http.Authorization

// NewApiTokenAuthorization carries a token somebody else obtained. Nothing refreshes it, so it
// might expire during long-running work.
func NewApiTokenAuthorization(apiToken string) Authorization {
	return http.BearerTokenAuthorization{Token: apiToken}
}

func New(ctx context.Context, rootUrl *url.URL, userAgent string, auth Authorization) (Client, error) {
	httpClient := internal.HttpClient{RootUrl: rootUrl, Client: http.NewClient(userAgent, auth)}

	infoClient := newMeshInfoClient(httpClient)
	if err := infoClient.checkMeshVersion(ctx); err != nil {
		return Client{}, err
	}

	return Client{
		Endpoint:                       rootUrl.String(),
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
		MeshInfo:                       infoClient,
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
