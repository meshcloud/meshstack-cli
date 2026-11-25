package client

type MeshWorkspaceUserGroup struct {
	Metadata MeshWorkspaceUserGroupMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshWorkspaceUserGroupSpec     `json:"spec" tfsdk:"spec"`
	Status   *MeshWorkspaceUserGroupStatus  `json:"status,omitzero" tfsdk:"status"`
}

type MeshWorkspaceUserGroupMetadata struct {
	Name             string  `json:"name" tfsdk:"name"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	SourceSystem     *string `json:"sourceSystem,omitzero" tfsdk:"source_system"`
}

func NewMeshWorkspaceUserGroupId(workspace, name string) string {
	return workspace + "." + name
}

type MeshWorkspaceUserGroupSpec struct {
	DisplayName string              `json:"displayName" tfsdk:"display_name"`
	Egid        *string             `json:"egid,omitzero" tfsdk:"egid"`
	Members     []string            `json:"members" tfsdk:"members"`
	Tags        map[string][]string `json:"tags" tfsdk:"tags"`
}

type MeshWorkspaceUserGroupStatus struct {
	DisabledUsernames []string `json:"disabledUsernames" tfsdk:"disabled_usernames"`
}

type MeshWorkspaceUserGroupFilter struct {
	WorkspaceIdentifier string `json:"workspaceIdentifier"`
	Username            string `json:"username"`
	SourceSystem        string `json:"sourceSystem"`
	Egid                string `json:"egid"`
	DisplayName         string `json:"displayName"`
}

type MeshWorkspaceUserGroupClient = MeshObjectClient[MeshWorkspaceUserGroup, MeshWorkspaceUserGroupFilter]

func (group MeshWorkspaceUserGroup) meshObjectId() string {
	return NewMeshWorkspaceUserGroupId(group.Metadata.OwnedByWorkspace, group.Metadata.Name)
}
