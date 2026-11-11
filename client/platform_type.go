package client

type MeshPlatformType struct {
	Metadata MeshPlatformTypeMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshPlatformTypeSpec     `json:"spec" tfsdk:"spec"`
	Status   MeshPlatformTypeStatus   `json:"status,omitzero" tfsdk:"status"`
}

type MeshPlatformTypeStatus struct {
	Lifecycle MeshPlatformTypeLifecycle `json:"lifecycle" tfsdk:"lifecycle"`
}

type MeshPlatformTypeLifecycle struct {
	State string `json:"state" tfsdk:"state"`
}

type MeshPlatformTypeMetadata struct {
	Name             string  `json:"name" tfsdk:"name"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	Uuid             *string `json:"uuid,omitzero" tfsdk:"uuid"`
}

type MeshPlatformTypeSpec struct {
	DisplayName     string  `json:"displayName" tfsdk:"display_name"`
	Category        string  `json:"category" tfsdk:"category"`
	DefaultEndpoint *string `json:"defaultEndpoint,omitzero" tfsdk:"default_endpoint"`
	Icon            string  `json:"icon" tfsdk:"icon"`
}

type MeshPlatformTypeFilter struct {
	Category        *string `json:"category"`
	LifecycleStatus *string `json:"lifecycleStatus"`
}

type MeshPlatformTypeClient = MeshObjectClient[MeshPlatformType, MeshPlatformTypeFilter]

func (platformType MeshPlatformType) meshObjectId() string {
	return platformType.Metadata.Name
}
