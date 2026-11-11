package client

import (
	"time"

	"github.com/meshcloud/meshstack-cli/client/types"
)

type MeshApiKeyMetadata = MeshObjectByUuidMetadata

type MeshApiKey struct {
	Metadata MeshApiKeyMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshApiKeySpec     `json:"spec" tfsdk:"spec"`
	Status   *MeshApiKeyStatus  `json:"status,omitzero" tfsdk:"status"`
}

func (k MeshApiKey) meshObjectId() string {
	return k.Metadata.meshObjectId()
}

type MeshApiKeySpec struct {
	DisplayName string                   `json:"displayName" tfsdk:"display_name"`
	Permissions types.Set[ApiPermission] `json:"permissions" tfsdk:"permissions"`
	ExpiresAt   *string                  `json:"expiresAt,omitzero" tfsdk:"expires_at"`
}

type MeshApiKeyStatus struct {
	ClientId     string  `json:"clientId" tfsdk:"client_id"`
	ClientSecret *string `json:"clientSecret,omitzero" tfsdk:"client_secret"`
	// The Terraform provider's meshstack_api_key schema has no attribute for ExpiresAt, and its
	// conversion fails on a tfsdk tag the schema does not have.
	ExpiresAt time.Time `json:"expiresAt,omitzero" tfsdk:"-"`
}

type MeshApiKeyClient = MeshObjectClient[MeshApiKey, NoFilter]
