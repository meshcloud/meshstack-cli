package client

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

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

func (m MeshWorkspaceUserGroupMetadata) Identifier() string {
	return MeshWorkspaceUserGroupIdentifier(m.OwnedByWorkspace, m.Name)
}

func MeshWorkspaceUserGroupIdentifier(ownedByWorkspace, name string) string {
	return ownedByWorkspace + "." + name
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

type MeshWorkspaceUserGroupListFilter struct {
	WorkspaceIdentifier string `json:"workspaceIdentifier"`
	Username            string `json:"username"`
	SourceSystem        string `json:"sourceSystem"`
}

type MeshWorkspaceUserGroupClient interface {
	List(ctx context.Context, filter MeshWorkspaceUserGroupListFilter) ([]MeshWorkspaceUserGroup, error)
	Read(ctx context.Context, ownedByWorkspace, name string) (*MeshWorkspaceUserGroup, error)
	Create(ctx context.Context, group *MeshWorkspaceUserGroup) (*MeshWorkspaceUserGroup, error)
	Update(ctx context.Context, group *MeshWorkspaceUserGroup) (*MeshWorkspaceUserGroup, error)
	Delete(ctx context.Context, ownedByWorkspace, name string) error
}

type meshWorkspaceUserGroupClient struct {
	meshObject internal.MeshObjectClient[MeshWorkspaceUserGroup]
}

func newWorkspaceUserGroupClient(ctx context.Context, httpClient internal.HttpClient) meshWorkspaceUserGroupClient {
	return meshWorkspaceUserGroupClient{internal.NewMeshObjectClient[MeshWorkspaceUserGroup](ctx, httpClient, "v1")}
}

func (c meshWorkspaceUserGroupClient) List(ctx context.Context, filter MeshWorkspaceUserGroupListFilter) ([]MeshWorkspaceUserGroup, error) {
	return c.meshObject.List(ctx, http.WithUrlQuery(filter))
}

func (c meshWorkspaceUserGroupClient) Read(ctx context.Context, ownedByWorkspace, name string) (*MeshWorkspaceUserGroup, error) {
	return c.meshObject.Get(ctx, MeshWorkspaceUserGroupIdentifier(ownedByWorkspace, name))
}

func (c meshWorkspaceUserGroupClient) Create(ctx context.Context, group *MeshWorkspaceUserGroup) (*MeshWorkspaceUserGroup, error) {
	return c.meshObject.Post(ctx, group)
}

func (c meshWorkspaceUserGroupClient) Update(ctx context.Context, group *MeshWorkspaceUserGroup) (*MeshWorkspaceUserGroup, error) {
	return c.meshObject.Put(ctx, group.Metadata.Identifier(), group)
}

func (c meshWorkspaceUserGroupClient) Delete(ctx context.Context, ownedByWorkspace, name string) error {
	return c.meshObject.Delete(ctx, MeshWorkspaceUserGroupIdentifier(ownedByWorkspace, name))
}
