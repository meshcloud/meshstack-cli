package client

type MeshWorkspaceGroupBinding struct {
	MeshWorkspaceBinding
}

type MeshWorkspaceGroupBindingClient = MeshObjectClient[MeshWorkspaceGroupBinding, MeshWorkspaceBindingFilter]

func (MeshWorkspaceGroupBinding) meshObjectPath() string {
	return "meshworkspacebindings/groupbindings"
}
