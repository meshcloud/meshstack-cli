package client

type MeshLocation struct {
	Metadata MeshLocationMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshLocationSpec     `json:"spec" tfsdk:"spec"`
	Status   MeshLocationStatus   `json:"status,omitzero" tfsdk:"status"`
}

type MeshLocationMetadata struct {
	Name             string  `json:"name" tfsdk:"name"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	Uuid             *string `json:"uuid,omitzero" tfsdk:"uuid"`
}

type MeshLocationSpec struct {
	DisplayName string `json:"displayName" tfsdk:"display_name"`
	Description string `json:"description" tfsdk:"description"`
}

type MeshLocationStatus struct {
	IsPublic bool `json:"isPublic" tfsdk:"is_public"`
}

type MeshLocationClient = MeshObjectClient[MeshLocation, NoFilter]

func (location MeshLocation) meshObjectId() string {
	return location.Metadata.Name
}
