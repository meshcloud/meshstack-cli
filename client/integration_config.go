package client

import (
	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/polymorph"
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
	polymorph.Polymorph[MeshIntegrationConfig, MeshIntegrationConfigType]

	Github      *MeshIntegrationGithubConfig      `json:"github,omitzero" polymorph:"github" tfsdk:"github"`
	Gitlab      *MeshIntegrationGitlabConfig      `json:"gitlab,omitzero" polymorph:"gitlab" tfsdk:"gitlab"`
	AzureDevops *MeshIntegrationAzureDevopsConfig `json:"azuredevops,omitzero" polymorph:"azuredevops" tfsdk:"azuredevops"`
	EntraId     *MeshIntegrationEntraIdConfig     `json:"entraid,omitzero" polymorph:"entraid" tfsdk:"entraid"`
}
