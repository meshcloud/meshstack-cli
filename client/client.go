package client

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/version"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
)

var MinMeshStackVersion = version.MustParse("2026.36.0")

type Version = version.Version

type HttpError = http.Error

type Client struct {
	ApiKey                         MeshApiKeyClient
	BuildingBlockV2                MeshBuildingBlockV2Client
	BuildingBlockRun               MeshBuildingBlockRunClient
	BuildingBlockDefinition        MeshBuildingBlockDefinitionClient
	BuildingBlockDefinitionVersion MeshBuildingBlockDefinitionVersionClient
	BuildingBlockRunner            MeshBuildingBlockRunnerClient
	EventLog                       MeshEventLogClient
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
	// HttpClient sends the bearer token to any host, so join its path onto Endpoint.
	HttpClient http.AuthorizedClient
}

func New(ctx context.Context, endpoint xurl.URL, client http.Client, auth http.Authorization) Client {
	c := client.WithAuthorization(auth)
	return Client{
		ApiKey:                         newMeshObjectClient[MeshApiKey, NoFilter](ctx, c, endpoint, "v1-preview"),
		BuildingBlockV2:                newMeshObjectClient[MeshBuildingBlockV2, MeshBuildingBlockV2Filter](ctx, c, endpoint, buildingBlockV2ApiVersion),
		BuildingBlockRun:               newMeshObjectClient[MeshBuildingBlockRun, MeshBuildingBlockRunFilter](ctx, c, endpoint, "v1"),
		BuildingBlockDefinition:        newMeshObjectClient[MeshBuildingBlockDefinition, MeshBuildingBlockDefinitionFilter](ctx, c, endpoint, "v1-preview"),
		BuildingBlockDefinitionVersion: newMeshObjectClient[MeshBuildingBlockDefinitionVersion, MeshBuildingBlockDefinitionVersionFilter](ctx, c, endpoint, "v1-preview"),
		BuildingBlockRunner:            newMeshObjectClient[MeshBuildingBlockRunner, MeshBuildingBlockRunnerFilter](ctx, c, endpoint, "v1-preview"),
		EventLog:                       newMeshObjectClient[MeshEventLog, MeshEventLogFilter](ctx, c, endpoint, "v1"),
		Integration:                    newMeshObjectClient[MeshIntegration, NoFilter](ctx, c, endpoint, "v1"),
		LandingZone:                    newMeshObjectClient[MeshLandingZone, MeshLandingZoneFilter](ctx, c, endpoint, "v1"),
		Location:                       newMeshObjectClient[MeshLocation, NoFilter](ctx, c, endpoint, "v1"),
		MeshInfo:                       NewMeshInfoClient(client, endpoint),
		PaymentMethod:                  newMeshObjectClient[MeshPaymentMethod, NoFilter](ctx, c, endpoint, "v2"),
		Platform:                       newMeshObjectClient[MeshPlatform, MeshPlatformFilter](ctx, c, endpoint, "v2"),
		PlatformType:                   newMeshObjectClient[MeshPlatformType, MeshPlatformTypeFilter](ctx, c, endpoint, "v1"),
		Project:                        newMeshObjectClient[MeshProject, MeshProjectFilter](ctx, c, endpoint, "v2"),
		ProjectGroupBinding:            newMeshObjectClient[MeshProjectGroupBinding, MeshProjectBindingFilter](ctx, c, endpoint, "v3"),
		ProjectUserBinding:             newMeshObjectClient[MeshProjectUserBinding, MeshProjectBindingFilter](ctx, c, endpoint, "v3"),
		ServiceInstance:                newMeshObjectClient[MeshServiceInstance, MeshServiceInstanceFilter](ctx, c, endpoint, "v2"),
		TagDefinition:                  newMeshObjectClient[MeshTagDefinition, NoFilter](ctx, c, endpoint, "v1"),
		Tenant:                         newMeshObjectClient[MeshTenant, MeshTenantFilter](ctx, c, endpoint, "v4"),
		Workspace:                      newMeshObjectClient[MeshWorkspace, NoFilter](ctx, c, endpoint, "v2"),
		WorkspaceGroupBinding:          newMeshObjectClient[MeshWorkspaceGroupBinding, MeshWorkspaceBindingFilter](ctx, c, endpoint, "v2"),
		WorkspaceUserBinding:           newMeshObjectClient[MeshWorkspaceUserBinding, MeshWorkspaceBindingFilter](ctx, c, endpoint, "v2"),

		Endpoint:   endpoint,
		HttpClient: c,
	}
}
