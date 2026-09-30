package client

type MeshProjectBinding struct {
	Metadata  MeshProjectBindingMetadata `json:"metadata" tfsdk:"metadata"`
	RoleRef   MeshProjectRoleRef         `json:"roleRef" tfsdk:"role_ref"`
	TargetRef MeshProjectTargetRef       `json:"targetRef" tfsdk:"target_ref"`
	Subject   MeshSubject                `json:"subject" tfsdk:"subject"`
}

type MeshProjectBindingMetadata struct {
	Name string `json:"name" tfsdk:"name"`
}

// MeshProjectRoleRef names a role by its name alone.
//
// Deprecated: use NamedRef, which also carries the kind. MeshProjectRoleRef is only for a
// meshObject whose API leaves the kind out.
type MeshProjectRoleRef struct {
	Name string `json:"name" tfsdk:"name"`
}

type MeshProjectTargetRef struct {
	Name             string `json:"name" tfsdk:"name"`
	OwnedByWorkspace string `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
}

type MeshSubject struct {
	Name string `json:"name" tfsdk:"name"`
}
