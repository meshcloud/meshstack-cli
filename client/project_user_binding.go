package client

import "github.com/meshcloud/meshstack-cli/client/types/enum"

type MeshProjectUserBinding struct {
	MeshProjectBinding

	Status *MeshUserBindingStatus `json:"status,omitempty" tfsdk:"status"`
}

type MeshUserBindingStatus struct {
	UserDisabled bool                            `json:"userDisabled" tfsdk:"user_disabled"`
	Approval     enum.Entry[MeshBindingApproval] `json:"approval,omitempty" tfsdk:"approval"`
}

type MeshProjectUserBindingClient = MeshObjectClient[MeshProjectUserBinding, MeshProjectBindingFilter]

func (MeshProjectUserBinding) meshObjectPath() string {
	return "meshprojectbindings/userbindings"
}
