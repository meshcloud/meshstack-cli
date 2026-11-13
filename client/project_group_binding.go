package client

type MeshProjectGroupBinding struct {
	MeshProjectBinding
}

type MeshProjectGroupBindingClient = MeshObjectClient[MeshProjectGroupBinding, MeshProjectBindingFilter]

func (MeshProjectGroupBinding) meshObjectPath() string {
	return "meshprojectbindings/groupbindings"
}
