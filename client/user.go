package client

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type MeshUser struct {
	Metadata MeshUserMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshUserSpec     `json:"spec" tfsdk:"spec"`
	Status   MeshUserStatus   `json:"status" tfsdk:"status"`
}

type MeshUserMetadata struct {
	Name string `json:"name" tfsdk:"name"`
	Uuid string `json:"uuid" tfsdk:"uuid"`
}

type MeshUserSpec struct {
	Email     string              `json:"email" tfsdk:"email"`
	FirstName string              `json:"firstName" tfsdk:"first_name"`
	LastName  string              `json:"lastName" tfsdk:"last_name"`
	Euid      *string             `json:"euid,omitzero" tfsdk:"euid"`
	Tags      map[string][]string `json:"tags" tfsdk:"tags"`
}

type MeshUserStatus struct {
	Lifecycle MeshUserLifecycle `json:"lifecycle" tfsdk:"lifecycle"`
	LastLogin *string           `json:"lastLogin,omitzero" tfsdk:"last_login"`
}

type MeshUserLifecycle struct {
	State string `json:"state" tfsdk:"state"`
}

type MeshUserListFilter struct {
	Uuid                string `json:"uuid"`
	Username            string `json:"username"`
	Email               string `json:"email"`
	Euid                string `json:"euid"`
	WorkspaceIdentifier string `json:"workspaceIdentifier"`
	WorkspaceRole       string `json:"workspaceRole"`
	ProjectIdentifier   string `json:"projectIdentifier"`
}

type MeshUserClient interface {
	List(ctx context.Context, filter MeshUserListFilter) ([]MeshUser, error)
	Read(ctx context.Context, username string) (*MeshUser, error)
}

type meshUserClient struct {
	meshObject internal.MeshObjectClient[MeshUser]
}

func newUserClient(ctx context.Context, httpClient internal.HttpClient) meshUserClient {
	return meshUserClient{internal.NewMeshObjectClient[MeshUser](ctx, httpClient, "v2")}
}

func (c meshUserClient) List(ctx context.Context, filter MeshUserListFilter) ([]MeshUser, error) {
	return c.meshObject.List(ctx, http.WithUrlQuery(filter))
}

func (c meshUserClient) Read(ctx context.Context, username string) (*MeshUser, error) {
	return c.meshObject.Get(ctx, username)
}
