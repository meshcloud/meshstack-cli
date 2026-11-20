package client

type MeshIntegrationMetadata = MeshObjectByUuidMetadata

type MeshIntegration struct {
	Metadata MeshIntegrationMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshIntegrationSpec     `json:"spec" tfsdk:"spec"`
	Status   *MeshIntegrationStatus  `json:"status,omitzero" tfsdk:"status"`
}

func (i MeshIntegration) meshObjectId() string {
	return i.Metadata.meshObjectId()
}

type MeshIntegrationSpec struct {
	DisplayName string                `json:"displayName" tfsdk:"display_name"`
	Config      MeshIntegrationConfig `json:"config" tfsdk:"config"`
}

type MeshIntegrationStatus struct {
	IsBuiltIn                  bool                            `json:"isBuiltIn" tfsdk:"is_built_in"`
	WorkloadIdentityFederation *MeshWorkloadIdentityFederation `json:"workloadIdentityFederation" tfsdk:"workload_identity_federation"`
}

type MeshWorkloadIdentityFederation struct {
	Issuer  string              `json:"issuer" tfsdk:"issuer"`
	Subject string              `json:"subject" tfsdk:"subject"`
	Gcp     *MeshWifProvider    `json:"gcp" tfsdk:"gcp"`
	Aws     *MeshAwsWifProvider `json:"aws" tfsdk:"aws"`
	Azure   *MeshWifProvider    `json:"azure" tfsdk:"azure"`
}

type MeshWifProvider struct {
	Audience string `json:"audience" tfsdk:"audience"`
}

type MeshAwsWifProvider struct {
	Audience   string `json:"audience" tfsdk:"audience"`
	Thumbprint string `json:"thumbprint" tfsdk:"thumbprint"`
}

type MeshIntegrationClient = MeshObjectClient[MeshIntegration, NoFilter]
