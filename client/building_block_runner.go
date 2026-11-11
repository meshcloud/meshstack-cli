package client

type MeshBuildingBlockRunnerImplementationType string

// TODO Turn this into enum!
const (
	MeshBuildingBlockRunnerImplementationTypeTerraform           MeshBuildingBlockRunnerImplementationType = "TERRAFORM"
	MeshBuildingBlockRunnerImplementationTypeGithubWorkflow      MeshBuildingBlockRunnerImplementationType = "GITHUB_WORKFLOW"
	MeshBuildingBlockRunnerImplementationTypeGitlabPipeline      MeshBuildingBlockRunnerImplementationType = "GITLAB_PIPELINE"
	MeshBuildingBlockRunnerImplementationTypeAzureDevopsPipeline MeshBuildingBlockRunnerImplementationType = "AZURE_DEVOPS_PIPELINE"
	MeshBuildingBlockRunnerImplementationTypeManual              MeshBuildingBlockRunnerImplementationType = "MANUAL"
	MeshBuildingBlockRunnerImplementationTypeAll                 MeshBuildingBlockRunnerImplementationType = "ALL"
)

var MeshBuildingBlockRunnerImplementationTypes = []string{
	string(MeshBuildingBlockRunnerImplementationTypeTerraform),
	string(MeshBuildingBlockRunnerImplementationTypeGithubWorkflow),
	string(MeshBuildingBlockRunnerImplementationTypeGitlabPipeline),
	string(MeshBuildingBlockRunnerImplementationTypeAzureDevopsPipeline),
	string(MeshBuildingBlockRunnerImplementationTypeManual),
	string(MeshBuildingBlockRunnerImplementationTypeAll),
}

type MeshBuildingBlockRunner struct {
	Metadata MeshBuildingBlockRunnerMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshBuildingBlockRunnerSpec     `json:"spec" tfsdk:"spec"`
	Status   *MeshBuildingBlockRunnerStatus  `json:"status,omitzero" tfsdk:"status"`
}

type MeshBuildingBlockRunnerMetadata struct {
	Uuid             *string `json:"uuid,omitzero" tfsdk:"uuid"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	CreatedOn        *string `json:"createdOn,omitzero" tfsdk:"created_on"`
}

type MeshBuildingBlockRunnerStatus struct {
	LastSeen         *string `json:"lastSeen,omitzero" tfsdk:"last_seen"`
	LastKnownVersion *string `json:"lastKnownVersion,omitzero" tfsdk:"last_known_version"`
}

type MeshBuildingBlockRunnerSpec struct {
	DisplayName                string                         `json:"displayName" tfsdk:"display_name"`
	PublicKey                  string                         `json:"publicKey" tfsdk:"public_key"`
	ImplementationType         string                         `json:"implementationType" tfsdk:"implementation_type"`
	Restriction                *string                        `json:"restriction,omitzero" tfsdk:"restriction"`
	IsSelfHosted               *bool                          `json:"isSelfHosted,omitzero" tfsdk:"is_self_hosted"`
	WorkloadIdentityFederation *MeshRunnerWorkloadIdentityFed `json:"workloadIdentityFederation,omitzero" tfsdk:"workload_identity_federation"`
}

type MeshRunnerWorkloadIdentityFed struct {
	// SubjectTemplate holds {{ workspaceIdentifier }} and {{ buildingBlockDefinitionUuid }}, which meshStack renders per building block definition version.
	SubjectTemplate *string                      `json:"subjectTemplate,omitzero" tfsdk:"subject_template"`
	Issuer          *string                      `json:"issuer,omitzero" tfsdk:"issuer"`
	Gcp             *MeshRunnerWifProviderConfig `json:"gcp,omitzero" tfsdk:"gcp"`
	Aws             *MeshRunnerWifProviderConfig `json:"aws,omitzero" tfsdk:"aws"`
	Azure           *MeshRunnerWifProviderConfig `json:"azure,omitzero" tfsdk:"azure"`
}

type MeshRunnerWifProviderConfig struct {
	Audience  string `json:"audience" tfsdk:"audience"`
	TokenPath string `json:"tokenPath" tfsdk:"token_path"`
}

type MeshBuildingBlockRunnerClient = MeshObjectClient[MeshBuildingBlockRunner, MeshBuildingBlockRunnerFilter]

type MeshBuildingBlockRunnerFilter struct {
	WorkspaceIdentifier *string `json:"workspaceIdentifier"`
}

func (runner MeshBuildingBlockRunner) meshObjectId() string {
	return *runner.Metadata.Uuid
}
