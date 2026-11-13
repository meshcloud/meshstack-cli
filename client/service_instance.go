package client

import (
	"github.com/meshcloud/meshstack-cli/client/types"
)

type MeshServiceInstance struct {
	Metadata MeshServiceInstanceMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshServiceInstanceSpec     `json:"spec" tfsdk:"spec"`
}

type MeshServiceInstanceMetadata struct {
	OwnedByProject        string `json:"ownedByProject" tfsdk:"owned_by_project"`
	OwnedByWorkspace      string `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	MarketplaceIdentifier string `json:"marketplaceIdentifier" tfsdk:"marketplace_identifier"`
	InstanceId            string `json:"instanceId" tfsdk:"instance_id"`
}

type MeshServiceInstanceSpec struct {
	Creator     string               `json:"creator" tfsdk:"creator"`
	DisplayName string               `json:"displayName" tfsdk:"display_name"`
	PlanId      string               `json:"planId" tfsdk:"plan_id"`
	ServiceId   string               `json:"serviceId" tfsdk:"service_id"`
	Parameters  map[string]types.Any `json:"parameters" tfsdk:"parameters"`
}

type MeshServiceInstanceClient = MeshObjectClient[MeshServiceInstance, MeshServiceInstanceFilter]

func (instance MeshServiceInstance) meshObjectId() string {
	return instance.Metadata.InstanceId
}

type MeshServiceInstanceFilter struct {
	WorkspaceIdentifier   *string `json:"workspaceIdentifier"`
	ProjectIdentifier     *string `json:"projectIdentifier"`
	MarketplaceIdentifier *string `json:"marketplaceIdentifier"`
	ServiceIdentifier     *string `json:"serviceIdentifier"`
	PlanIdentifier        *string `json:"planIdentifier"`
}
