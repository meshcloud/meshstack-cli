package client

type MeshWorkspaceBinding struct {
	Metadata   MeshWorkspaceBindingMetadata `json:"metadata" tfsdk:"metadata"`
	RoleRef    MeshWorkspaceRoleRef         `json:"roleRef" tfsdk:"role_ref"`
	TargetRef  MeshWorkspaceTargetRef       `json:"targetRef" tfsdk:"target_ref"`
	Subject    MeshWorkspaceSubject         `json:"subject" tfsdk:"subject"`
	ExpiryDate *string                      `json:"expiryDate,omitzero" tfsdk:"expiry_date"`
}

type MeshWorkspaceBindingMetadata struct {
	Name string `json:"name" tfsdk:"name"`
}

type MeshWorkspaceRoleRef struct {
	Name string `json:"name" tfsdk:"name"`
}

type MeshWorkspaceTargetRef struct {
	Name string `json:"name" tfsdk:"name"`
}

type MeshWorkspaceSubject struct {
	Name string `json:"name" tfsdk:"name"`
}

type MeshWorkspaceBindingListFilter struct {
	WorkspaceIdentifier *string `json:"workspaceIdentifier"`
	WorkspaceRole       *string `json:"workspaceRole"`
	// Username matches a user binding of that user, or a group binding of a group the user is in.
	Username *string `json:"username"`
}
