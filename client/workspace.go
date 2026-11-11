package client

import "github.com/meshcloud/meshstack-cli/client/types/hal"

type MeshWorkspace struct {
	Metadata MeshWorkspaceMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshWorkspaceSpec     `json:"spec" tfsdk:"spec"`
	Links    hal.Links             `json:"_links,omitzero" tfsdk:"-"`
}

type MeshWorkspaceMetadata struct {
	Name      string              `json:"name" tfsdk:"name"`
	CreatedOn string              `json:"createdOn,omitzero" tfsdk:"created_on"`
	DeletedOn *string             `json:"deletedOn,omitzero" tfsdk:"deleted_on"`
	Tags      map[string][]string `json:"tags" tfsdk:"tags"`
}

type MeshWorkspaceSpec struct {
	DisplayName                  string `json:"displayName" tfsdk:"display_name"`
	PlatformBuilderAccessEnabled *bool  `json:"platformBuilderAccessEnabled,omitzero" tfsdk:"platform_builder_access_enabled"`
}

type MeshWorkspaceClient = MeshObjectClient[MeshWorkspace, NoFilter]

func (workspace MeshWorkspace) meshObjectId() string {
	return workspace.Metadata.Name
}
