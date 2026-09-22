package client

import (
	"encoding/json/v2"
	"fmt"

	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type MeshIntegrationConfigType string

var (
	MeshIntegrationConfigTypes           = enum.Enum[MeshIntegrationConfigType]{}
	MeshIntegrationConfigTypeGithub      = MeshIntegrationConfigTypes.Entry("github")
	MeshIntegrationConfigTypeGitlab      = MeshIntegrationConfigTypes.Entry("gitlab")
	MeshIntegrationConfigTypeAzureDevops = MeshIntegrationConfigTypes.Entry("azuredevops")
	MeshIntegrationConfigTypeEntraId     = MeshIntegrationConfigTypes.Entry("entraid")
)

type MeshIntegrationGithubConfig struct {
	Owner         string       `json:"owner" tfsdk:"owner"`
	BaseUrl       string       `json:"baseUrl" tfsdk:"base_url"`
	AppId         string       `json:"appId" tfsdk:"app_id"`
	AppPrivateKey types.Secret `json:"appPrivateKey" tfsdk:"app_private_key"`
	RunnerRef     *UuidRef     `json:"runnerRef" tfsdk:"runner_ref"`
}

type MeshIntegrationGitlabConfig struct {
	BaseUrl   string   `json:"baseUrl" tfsdk:"base_url"`
	RunnerRef *UuidRef `json:"runnerRef" tfsdk:"runner_ref"`
}

type MeshIntegrationAzureDevopsConfig struct {
	BaseUrl             string       `json:"baseUrl" tfsdk:"base_url"`
	Organization        string       `json:"organization" tfsdk:"organization"`
	PersonalAccessToken types.Secret `json:"personalAccessToken" tfsdk:"personal_access_token"`
	RunnerRef           *UuidRef     `json:"runnerRef" tfsdk:"runner_ref"`
}

type MeshIntegrationEntraIdConfig struct {
	TenantId     string       `json:"tenantId" tfsdk:"tenant_id"`
	ClientId     string       `json:"clientId" tfsdk:"client_id"`
	ClientSecret types.Secret `json:"clientSecret" tfsdk:"client_secret"`
	IdpAlias     *string      `json:"idpAlias,omitzero" tfsdk:"idp_alias"`
	// meshStack derives this and returns it inside spec, which configuration writes. A computed value
	// there is unreachable under provider mocks (issue #272), so Terraform reads it from status instead.
	RedirectUrl *string `json:"redirectUrl,omitzero" tfsdk:"-"`
}

type MeshIntegrationConfig struct {
	Type        enum.Entry[MeshIntegrationConfigType] `json:"type" tfsdk:"-"`
	Github      *MeshIntegrationGithubConfig          `json:"github,omitzero" tfsdk:"github"`
	Gitlab      *MeshIntegrationGitlabConfig          `json:"gitlab,omitzero" tfsdk:"gitlab"`
	AzureDevops *MeshIntegrationAzureDevopsConfig     `json:"azuredevops,omitzero" tfsdk:"azuredevops"`
	EntraId     *MeshIntegrationEntraIdConfig         `json:"entraid,omitzero" tfsdk:"entraid"`
}

func (m MeshIntegrationConfig) InferType() (enum.Entry[MeshIntegrationConfigType], error) {
	result, err := inferVariantType(
		variant(MeshIntegrationConfigTypeGithub, m.Github != nil),
		variant(MeshIntegrationConfigTypeGitlab, m.Gitlab != nil),
		variant(MeshIntegrationConfigTypeAzureDevops, m.AzureDevops != nil),
		variant(MeshIntegrationConfigTypeEntraId, m.EntraId != nil),
	)
	if err != nil {
		return "", fmt.Errorf("cannot infer integration config type: %w", err)
	}
	return result, nil
}

func (m MeshIntegrationConfig) MarshalJSON() ([]byte, error) {
	// Using wrapped type avoids calling MarshalJSON recursively!
	type wrapped MeshIntegrationConfig
	w := wrapped(m)
	// Built-in integrations (replicator, metering) come with a type but no variant, so the type is only
	// inferred when it is missing.
	if len(w.Type) == 0 {
		inferred, err := m.InferType()
		if err != nil {
			return nil, err
		}
		w.Type = inferred
	}
	return json.Marshal(w, wireCompatibility)
}

func (m *MeshIntegrationConfig) UnmarshalJSON(bytes []byte) error {
	type wrapped MeshIntegrationConfig
	var target wrapped
	if err := json.Unmarshal(bytes, &target); err != nil {
		return err
	}
	*m = MeshIntegrationConfig(target)
	return nil
}
