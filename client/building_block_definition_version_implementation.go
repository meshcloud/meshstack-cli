package client

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"

	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type MeshBuildingBlockImplementationType string

var (
	MeshBuildingBlockImplementationTypes                   = enum.Enum[MeshBuildingBlockImplementationType]{}
	MeshBuildingBlockImplementationTypeManual              = MeshBuildingBlockImplementationTypes.Entry("manual")
	MeshBuildingBlockImplementationTypeTerraform           = MeshBuildingBlockImplementationTypes.Entry("terraform")
	MeshBuildingBlockImplementationTypeGithubWorkflows     = MeshBuildingBlockImplementationTypes.Entry("githubWorkflows")
	MeshBuildingBlockImplementationTypeGitlabPipeline      = MeshBuildingBlockImplementationTypes.Entry("gitlabPipeline")
	MeshBuildingBlockImplementationTypeAzureDevOpsPipeline = MeshBuildingBlockImplementationTypes.Entry("azureDevOpsPipeline")
)

type MeshBuildingBlockDefinitionSshKnownHost struct {
	Host     string `json:"host" tfsdk:"host"`
	KeyType  string `json:"keyType" tfsdk:"key_type"`
	KeyValue string `json:"keyValue" tfsdk:"key_value"`
}

type MeshBuildingBlockDefinitionTerraformImplementation struct {
	TerraformVersion           string                                   `json:"terraformVersion" tfsdk:"terraform_version"`
	RepositoryURL              string                                   `json:"repositoryUrl" tfsdk:"repository_url"`
	Async                      bool                                     `json:"async" tfsdk:"async"`
	RepositoryPath             *string                                  `json:"repositoryPath,omitzero" tfsdk:"repository_path"`
	RefName                    *string                                  `json:"refName,omitzero" tfsdk:"ref_name"`
	SSHKnownHost               *MeshBuildingBlockDefinitionSshKnownHost `json:"sshKnownHost,omitzero" tfsdk:"ssh_known_host"`
	UseMeshHTTPBackendFallback bool                                     `json:"useMeshHttpBackendFallback" tfsdk:"use_mesh_http_backend_fallback"`
	SSHPrivateKey              *types.Secret                            `json:"sshPrivateKey,omitzero" tfsdk:"ssh_private_key"`
	PreRunScript               *string                                  `json:"preRunScript,omitzero" tfsdk:"pre_run_script"`
}

type MeshBuildingBlockDefinitionGitHubWorkflowsImplementation struct {
	Repository         string  `json:"repository" tfsdk:"repository"`
	Branch             string  `json:"branch" tfsdk:"branch"`
	ApplyWorkflow      string  `json:"applyWorkflow" tfsdk:"apply_workflow"`
	DestroyWorkflow    *string `json:"destroyWorkflow" tfsdk:"destroy_workflow"`
	Async              bool    `json:"async" tfsdk:"async"`
	OmitRunObjectInput bool    `json:"omitRunObjectInput" tfsdk:"omit_run_object_input"`
	IntegrationRef     UuidRef `json:"integrationRef" tfsdk:"integration_ref"`
}

type MeshBuildingBlockDefinitionManualImplementation struct{}

type MeshBuildingBlockDefinitionGitLabPipelineImplementation struct {
	ProjectID            string       `json:"projectId" tfsdk:"project_id"`
	RefName              string       `json:"refName" tfsdk:"ref_name"`
	IntegrationRef       UuidRef      `json:"integrationRef" tfsdk:"integration_ref"`
	PipelineTriggerToken types.Secret `json:"pipelineTriggerToken" tfsdk:"pipeline_trigger_token"`
}

type MeshBuildingBlockDefinitionAzureDevOpsPipelineImplementation struct {
	Project        string  `json:"project" tfsdk:"project"`
	PipelineID     string  `json:"pipelineId" tfsdk:"pipeline_id"`
	RefName        *string `json:"refName,omitzero" tfsdk:"ref_name"`
	Async          bool    `json:"async" tfsdk:"async"`
	IntegrationRef UuidRef `json:"integrationRef" tfsdk:"integration_ref"`
}

type MeshBuildingBlockDefinitionImplementation struct {
	Type                enum.Entry[MeshBuildingBlockImplementationType]               `json:"type" tfsdk:"-"`
	Manual              *MeshBuildingBlockDefinitionManualImplementation              `json:"manual,omitzero" tfsdk:"manual"`
	GithubWorkflows     *MeshBuildingBlockDefinitionGitHubWorkflowsImplementation     `json:"githubWorkflows,omitzero" tfsdk:"github_workflows"`
	AzureDevOpsPipeline *MeshBuildingBlockDefinitionAzureDevOpsPipelineImplementation `json:"azureDevOpsPipeline,omitzero" tfsdk:"azure_devops_pipeline"`
	GitlabPipeline      *MeshBuildingBlockDefinitionGitLabPipelineImplementation      `json:"gitlabPipeline,omitzero" tfsdk:"gitlab_pipeline"`
	Terraform           *MeshBuildingBlockDefinitionTerraformImplementation           `json:"terraform,omitzero" tfsdk:"terraform"`
}

// InferTypeFromNonNilField panics when no variant is set. Callers hold a plan or state, where the schema
// guarantees exactly one variant; a response from the API goes through MarshalJSON, which reports the
// error instead.
func (m MeshBuildingBlockDefinitionImplementation) InferTypeFromNonNilField() enum.Entry[MeshBuildingBlockImplementationType] {
	result, err := m.inferType()
	if err != nil {
		panic(err)
	}
	return result
}

func (m MeshBuildingBlockDefinitionImplementation) inferType() (result enum.Entry[MeshBuildingBlockImplementationType], err error) {
	setResultIfNotNil := func(implType enum.Entry[MeshBuildingBlockImplementationType], v any) {
		// Manual implementation is an empty struct, so carefully check v for nilness using reflection!
		if !reflect.ValueOf(v).IsZero() {
			if len(result) > 0 && result != implType {
				err = fmt.Errorf("inferred implementation type %s but already set to %s", implType, result)
			}
			result = implType
		}
	}
	setResultIfNotNil(MeshBuildingBlockImplementationTypeManual, m.Manual)
	setResultIfNotNil(MeshBuildingBlockImplementationTypeTerraform, m.Terraform)
	setResultIfNotNil(MeshBuildingBlockImplementationTypeGithubWorkflows, m.GithubWorkflows)
	setResultIfNotNil(MeshBuildingBlockImplementationTypeGitlabPipeline, m.GitlabPipeline)
	setResultIfNotNil(MeshBuildingBlockImplementationTypeAzureDevOpsPipeline, m.AzureDevOpsPipeline)
	if err != nil {
		return "", err
	}
	if len(result) == 0 {
		// meshStack answers a workspace that may only consume a definition with versions that carry no implementation.
		return "", errors.New("cannot infer implementation type: no implementation variant is set")
	}
	return result, nil
}

func (m MeshBuildingBlockDefinitionImplementation) MarshalJSON() ([]byte, error) {
	type wrapped MeshBuildingBlockDefinitionImplementation
	w := wrapped(m)
	if len(w.Type) == 0 {
		inferred, err := m.inferType()
		if err != nil {
			return nil, err
		}
		w.Type = inferred
	}
	return json.Marshal(w, wireCompatibility)
}

func (m *MeshBuildingBlockDefinitionImplementation) UnmarshalJSON(bytes []byte) error {
	type wrapped MeshBuildingBlockDefinitionImplementation
	var target wrapped
	if err := json.Unmarshal(bytes, &target); err != nil {
		return err
	}
	*m = MeshBuildingBlockDefinitionImplementation(target)
	if m.Type == MeshBuildingBlockImplementationTypeManual {
		m.Manual = &MeshBuildingBlockDefinitionManualImplementation{}
	}
	return nil
}
