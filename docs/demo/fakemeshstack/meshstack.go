package main

import (
	"crypto/sha256"
	_ "embed"
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

type installation struct {
	profile          string
	api, sso         string
	defaultWorkspace string
}

var installations = []installation{
	{profile: "production", api: "api.meshstack.example.com", sso: "sso.meshstack.example.com", defaultWorkspace: "platform-team"},
	{profile: "staging", api: "api.staging.meshstack.example.com", sso: "sso.staging.meshstack.example.com"},
	{profile: "dev", api: "api.dev.meshstack.example.com", sso: "sso.dev.meshstack.example.com"},
}

func (i installation) endpoint() string { return "https://" + i.api }

func (i installation) meshStack() *fakemeshstack.Server {
	return fakemeshstack.New(fakemeshstack.Options{
		AdminWorkspace: "platform-team",
		Issuer:         "https://" + i.sso + "/realms/meshfed",
		Workspaces:     workspaces(),
		BuildingBlocks: buildingBlocks(),
	})
}

// apiDocs is meshStack's OpenAPI document cut down to a few building block operations with a few
// fields each, so that a description of them fits the gif.
//
//go:embed api-docs.json
var apiDocs []byte

const docsHost = "docs.meshstack.example.com"

func workspaces() (objects []any) {
	for _, workspace := range [][2]string{
		{"platform-team", "Platform Team"},
		{"customer-portal", "Customer Portal"},
		{"data-analytics", "Data Analytics"},
		{"payments", "Payments"},
		{"mobile-apps", "Mobile Apps"},
		{"machine-learning", "Machine Learning"},
		{"security-ops", "Security Operations"},
		{"identity-access", "Identity & Access"},
		{"marketing-web", "Marketing Websites"},
		{"internal-tools", "Internal Tools"},
		{"billing", "Billing Services"},
		{"search", "Search & Discovery"},
		{"logistics", "Logistics"},
		{"hr-systems", "HR Systems"},
		{"devex", "Developer Experience"},
		{"observability", "Observability"},
	} {
		objects = append(objects, map[string]any{
			"metadata": map[string]any{"name": workspace[0], "createdOn": "2025-03-14T09:26:53Z", "tags": map[string]any{}},
			"spec":     map[string]any{"displayName": workspace[1]},
		})
	}
	return objects
}

type buildingBlock struct {
	Metadata struct {
		Uuid             string `json:"uuid"`
		OwnedByWorkspace string `json:"ownedByWorkspace"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName"`
	} `json:"spec"`
	Status struct {
		Status string `json:"status"`
	} `json:"status"`
}

// buildingBlocks are more than a demo's --limit, so that the CLI says it listed only the first ones.
func buildingBlocks() (objects []any) {
	for _, block := range [][3]string{
		{"PostgreSQL Database", "platform-team", "SUCCEEDED"},
		{"Azure Storage Account", "data-analytics", "SUCCEEDED"},
		{"GitHub Repository", "customer-portal", "SUCCEEDED"},
		{"Kubernetes Namespace", "payments", "IN_PROGRESS"},
		{"AWS S3 Bucket", "machine-learning", "SUCCEEDED"},
		{"Key Vault", "security-ops", "FAILED"},
		{"CI/CD Pipeline", "mobile-apps", "SUCCEEDED"},
		{"Redis Cache", "billing", "SUCCEEDED"},
		{"Budget Alert", "marketing-web", "SUCCEEDED"},
		{"Container Registry", "devex", "SUCCEEDED"},
		{"Log Analytics Workspace", "observability", "SUCCEEDED"},
		{"Service Principal", "identity-access", "SUCCEEDED"},
		{"PostgreSQL Database", "logistics", "SUCCEEDED"},
		{"GitHub Repository", "internal-tools", "SUCCEEDED"},
		{"Kubernetes Namespace", "search", "SUCCEEDED"},
		{"DNS Zone", "customer-portal", "SUCCEEDED"},
		{"AWS S3 Bucket", "hr-systems", "WAITING_FOR_OPERATOR_INPUT"},
		{"Budget Alert", "payments", "SUCCEEDED"},
		{"Key Vault", "platform-team", "SUCCEEDED"},
		{"CI/CD Pipeline", "data-analytics", "SUCCEEDED"},
		{"Redis Cache", "customer-portal", "SUCCEEDED"},
		{"Container Registry", "platform-team", "SUCCEEDED"},
		{"DNS Zone", "marketing-web", "SUCCEEDED"},
	} {
		var object buildingBlock
		object.Metadata.Uuid, object.Metadata.OwnedByWorkspace = stableUuid(block[0]+block[1]), block[1]
		object.Spec.DisplayName, object.Status.Status = block[0], block[2]
		objects = append(objects, object)
	}
	return objects
}

// stableUuid keeps the recorded output the same from one recording to the next.
func stableUuid(of string) string {
	sum := sha256.Sum256([]byte(of))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
