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
		Action string `json:"action"`
	} `json:"status"`
}

// buildingBlocks are more than a demo's --limit, so that the CLI says it listed only the first ones.
func buildingBlocks() (objects []any) {
	for _, block := range [][3]string{
		{"PostgreSQL Database", "platform-team", "NO_ACTION"},
		{"Azure Storage Account", "data-analytics", "NO_ACTION"},
		{"GitHub Repository", "customer-portal", "NO_ACTION"},
		{"Kubernetes Namespace", "payments", "IN_PROGRESS"},
		{"AWS S3 Bucket", "machine-learning", "NO_ACTION"},
		{"Key Vault", "security-ops", "REVIEW_ERRORS"},
		{"CI/CD Pipeline", "mobile-apps", "NO_ACTION"},
		{"Redis Cache", "billing", "NO_ACTION"},
		{"Budget Alert", "marketing-web", "NO_ACTION"},
		{"Container Registry", "devex", "NO_ACTION"},
		{"Log Analytics Workspace", "observability", "NO_ACTION"},
		{"Service Principal", "identity-access", "NO_ACTION"},
		{"PostgreSQL Database", "logistics", "NO_ACTION"},
		{"GitHub Repository", "internal-tools", "NO_ACTION"},
		{"Kubernetes Namespace", "search", "NO_ACTION"},
		{"DNS Zone", "customer-portal", "NO_ACTION"},
		{"AWS S3 Bucket", "hr-systems", "PROVIDE_INPUT"},
		{"Budget Alert", "payments", "NO_ACTION"},
		{"Key Vault", "platform-team", "NO_ACTION"},
		{"CI/CD Pipeline", "data-analytics", "NO_ACTION"},
		{"Redis Cache", "customer-portal", "NO_ACTION"},
		{"Container Registry", "platform-team", "NO_ACTION"},
		{"DNS Zone", "marketing-web", "NO_ACTION"},
	} {
		var object buildingBlock
		object.Metadata.Uuid, object.Metadata.OwnedByWorkspace = stableUuid(block[0]+block[1]), block[1]
		object.Spec.DisplayName, object.Status.Action = block[0], block[2]
		objects = append(objects, object)
	}
	return objects
}

// stableUuid keeps the recorded output the same from one recording to the next.
func stableUuid(of string) string {
	sum := sha256.Sum256([]byte(of))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
