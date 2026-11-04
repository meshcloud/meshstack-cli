package client

import "github.com/meshcloud/meshstack-cli/client/types/enum"

type MeshBindingApproval string

var (
	MeshBindingApprovals        = enum.Enum[MeshBindingApproval]{}
	MeshBindingApprovalAssigned = MeshBindingApprovals.Entry("ASSIGNED")
	MeshBindingApprovalPending  = MeshBindingApprovals.Entry("PENDING")
)

type MeshGroupBindingStatus struct {
	Approval enum.Entry[MeshBindingApproval] `json:"approval,omitempty" tfsdk:"approval"`
}

func (s *MeshGroupBindingStatus) WaitsForApproval() bool {
	return s != nil && s.Approval == MeshBindingApprovalPending
}

func (s *MeshUserBindingStatus) WaitsForApproval() bool {
	return s != nil && s.Approval == MeshBindingApprovalPending
}
