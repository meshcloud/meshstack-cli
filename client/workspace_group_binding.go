package client

type MeshWorkspaceGroupBinding struct {
	MeshWorkspaceBinding

	Status *MeshGroupBindingStatus `json:"status,omitempty" tfsdk:"status"`
}

type MeshWorkspaceGroupBindingClient = MeshObjectClient[MeshWorkspaceGroupBinding, MeshWorkspaceBindingFilter]

func (MeshWorkspaceGroupBinding) meshObjectPath() string {
	return "meshworkspacebindings/groupbindings"
}
