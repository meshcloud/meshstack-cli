package client

type MeshProjectGroupBinding struct {
	MeshProjectBinding

	Status *MeshGroupBindingStatus `json:"status,omitempty" tfsdk:"status"`
}

type MeshProjectGroupBindingClient = MeshObjectClient[MeshProjectGroupBinding, MeshProjectBindingFilter]

func (MeshProjectGroupBinding) meshObjectPath() string {
	return "meshprojectbindings/groupbindings"
}
