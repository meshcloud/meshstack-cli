package client

import (
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/polymorph"
)

type MeshBuildingBlockRunImplementationType string

var (
	MeshBuildingBlockRunImplementationTypes              = enum.Enum[MeshBuildingBlockRunImplementationType]{}
	MeshBuildingBlockRunImplementationTypeManual         = MeshBuildingBlockRunImplementationTypes.Entry("MANUAL")
	MeshBuildingBlockRunImplementationTypeTerraform      = MeshBuildingBlockRunImplementationTypes.Entry("TERRAFORM")
	MeshBuildingBlockRunImplementationTypeGithubWorkflow = MeshBuildingBlockRunImplementationTypes.Entry("GITHUB_WORKFLOW")
	MeshBuildingBlockRunImplementationTypeGitlabCiCd     = MeshBuildingBlockRunImplementationTypes.Entry("GITLAB_CICD")
	MeshBuildingBlockRunImplementationTypeAzureDevops    = MeshBuildingBlockRunImplementationTypes.Entry("AZURE_DEVOPS")
)

type MeshBuildingBlockRunImplementation struct {
	polymorph.Polymorph[MeshBuildingBlockRunImplementation, MeshBuildingBlockRunImplementationType]

	Manual         *MeshBuildingBlockRunManualImplementation      `polymorph:"MANUAL,inline"`
	Terraform      *MeshBuildingBlockRunTerraformImplementation   `polymorph:"TERRAFORM,inline"`
	GithubWorkflow *MeshBuildingBlockRunGithubImplementation      `polymorph:"GITHUB_WORKFLOW,inline"`
	GitlabCiCd     *MeshBuildingBlockRunGitlabImplementation      `polymorph:"GITLAB_CICD,inline"`
	AzureDevops    *MeshBuildingBlockRunAzureDevopsImplementation `polymorph:"AZURE_DEVOPS,inline"`
}

type MeshBuildingBlockRunManualImplementation struct{}

type MeshBuildingBlockRunTerraformImplementation struct {
	TerraformVersion           string                         `json:"terraformVersion"`
	RepositoryUrl              string                         `json:"repositoryUrl"`
	RepositoryPath             *string                        `json:"repositoryPath,omitzero"`
	RefName                    *string                        `json:"refName,omitzero"`
	SshPrivateKey              *string                        `json:"sshPrivateKey,omitzero"`
	KnownHost                  *MeshBuildingBlockRunKnownHost `json:"knownHost,omitzero"`
	UseMeshHttpBackendFallback bool                           `json:"useMeshHttpBackendFallback"`
	PreRunScript               *string                        `json:"preRunScript,omitzero"`
	Async                      bool                           `json:"async"`
}

type MeshBuildingBlockRunKnownHost struct {
	Host     string `json:"host"`
	KeyType  string `json:"keyType"`
	KeyValue string `json:"keyValue"`
}

type MeshBuildingBlockRunGithubImplementation struct {
	GithubBaseUrl      string  `json:"githubBaseUrl"`
	Owner              string  `json:"owner"`
	AppId              string  `json:"appId"`
	AppPem             string  `json:"appPem"`
	Repository         string  `json:"repository"`
	Branch             string  `json:"branch"`
	ApplyWorkflow      string  `json:"applyWorkflow"`
	DestroyWorkflow    *string `json:"destroyWorkflow,omitzero"`
	Async              bool    `json:"async"`
	OmitRunObjectInput bool    `json:"omitRunObjectInput"`
}

type MeshBuildingBlockRunGitlabImplementation struct {
	GitlabBaseUrl        string `json:"gitlabBaseUrl"`
	ProjectId            string `json:"projectId"`
	RefName              string `json:"refName"`
	PipelineTriggerToken string `json:"pipelineTriggerToken"`
}

type MeshBuildingBlockRunAzureDevopsImplementation struct {
	AzureDevOpsBaseUrl  string  `json:"azureDevOpsBaseUrl"`
	Organization        string  `json:"organization"`
	Project             string  `json:"project"`
	PipelineId          string  `json:"pipelineId"`
	PersonalAccessToken string  `json:"personalAccessToken"`
	Async               bool    `json:"async"`
	RefName             *string `json:"refName,omitzero"`
}
