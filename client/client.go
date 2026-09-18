package client

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/version"
)

var MinMeshStackVersion = version.MustParse("2026.36.0")

// Version is re-exported because MinMeshStackVersion is one and internal/version is closed to
// another module.
type Version = version.Version

// HttpError represents an HTTP error response with status code.
// This error is returned when an HTTP request fails with a non-2XX status code.
type HttpError = http.Error

type Authorization = http.Authorization

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

	// Endpoint is read by the Terraform provider's meshstack_instance data source.
	Endpoint xurl.URL
}

func New(ctx context.Context, endpoint xurl.URL, userAgent string, auth Authorization) Client {
	client := http.NewClient(userAgent)
	authorizedClient := internal.HttpClient{
		AuthorizedClient: client.WithAuthorization(auth),
		EndpointUrl:      endpoint,
	}
	return Client{
		ApiKey:                         newApiKeyClient(ctx, authorizedClient),
		BuildingBlock:                  newBuildingBlockClient(ctx, authorizedClient),
		BuildingBlockV2:                newBuildingBlockV2Client(ctx, authorizedClient),
		BuildingBlockRun:               newBuildingBlockRunClient(ctx, authorizedClient),
		BuildingBlockDefinition:        newBuildingBlockDefinitionClient(ctx, authorizedClient),
		BuildingBlockDefinitionVersion: newBuildingBlockDefinitionVersionClient(ctx, authorizedClient),
		BuildingBlockRunner:            newBuildingBlockRunnerClient(ctx, authorizedClient),
		Integration:                    newIntegrationClient(ctx, authorizedClient),
		LandingZone:                    newLandingZoneClient(ctx, authorizedClient),
		Location:                       newLocationClient(ctx, authorizedClient),
		MeshInfo:                       NewMeshInfoClient(client, endpoint),
		PaymentMethod:                  newPaymentMethodClient(ctx, authorizedClient),
		Platform:                       newPlatformClient(ctx, authorizedClient),
		PlatformType:                   newPlatformTypeClient(ctx, authorizedClient),
		Project:                        newProjectClient(ctx, authorizedClient),
		ProjectGroupBinding:            newProjectGroupBindingClient(ctx, authorizedClient),
		ProjectUserBinding:             newProjectUserBindingClient(ctx, authorizedClient),
		ServiceInstance:                newServiceInstanceClient(ctx, authorizedClient),
		TagDefinition:                  newTagDefinitionClient(ctx, authorizedClient),
		Tenant:                         newTenantClient(ctx, authorizedClient),
		Workspace:                      newWorkspaceClient(ctx, authorizedClient),
		WorkspaceGroupBinding:          newWorkspaceGroupBindingClient(ctx, authorizedClient),
		WorkspaceUserBinding:           newWorkspaceUserBindingClient(ctx, authorizedClient),

		Endpoint: endpoint,
	}
}
