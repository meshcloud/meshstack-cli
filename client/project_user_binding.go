package client

type MeshProjectUserBinding struct {
	MeshProjectBinding

	Status *MeshUserBindingStatus `json:"status,omitempty" tfsdk:"status"`
}

type MeshUserBindingStatus struct {
	UserDisabled bool `json:"userDisabled" tfsdk:"user_disabled"`
}

type MeshProjectUserBindingClient = MeshObjectClient[MeshProjectUserBinding, MeshProjectBindingFilter]

func (MeshProjectUserBinding) meshObjectPath() string {
	return "meshprojectbindings/userbindings"
}
