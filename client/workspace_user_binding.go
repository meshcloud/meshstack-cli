package client

type MeshWorkspaceUserBinding struct {
	MeshWorkspaceBinding

	Status *MeshUserBindingStatus `json:"status,omitempty" tfsdk:"status"`
}

type MeshWorkspaceUserBindingClient = MeshObjectClient[MeshWorkspaceUserBinding, MeshWorkspaceBindingFilter]

func (MeshWorkspaceUserBinding) meshObjectPath() string {
	return "meshworkspacebindings/userbindings"
}
