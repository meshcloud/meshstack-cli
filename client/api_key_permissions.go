package client

import (
	"slices"
	"strings"
)

type ApiPermission string

type ApiKeyPermissions []ApiKeyPermissionGroup

// ApiKeyPermissionGroup holds the permissions of one resource: one entry per action, and in that
// the scope variants of the action, such as TENANT_DELETE and ADM_TENANT_DELETE.
type ApiKeyPermissionGroup struct {
	Name    string
	Actions [][]ApiPermission
}

func (p ApiKeyPermissions) AllCodes() []string {
	var codes []string
	for _, group := range p {
		for _, suffixGroup := range group.Actions {
			for _, code := range suffixGroup {
				codes = append(codes, string(code))
			}
		}
	}
	return codes
}

// WorkspaceCodes leaves out only the ADM_ codes, so the MANAGED_ codes are part of it.
func (p ApiKeyPermissions) WorkspaceCodes() []string {
	var codes []string
	for _, group := range p {
		for _, suffixGroup := range group.Actions {
			for _, code := range suffixGroup {
				if !strings.HasPrefix(string(code), "ADM_") {
					codes = append(codes, string(code))
				}
			}
		}
	}
	return codes
}

// Only keeps the codes of p that are among codes, in the order of p, and puts the codes p does
// not know into a last group named Other.
func (p ApiKeyPermissions) Only(codes []ApiPermission) ApiKeyPermissions {
	remaining := slices.Clone(codes)
	var only ApiKeyPermissions
	for _, group := range p {
		kept := ApiKeyPermissionGroup{Name: group.Name}
		for _, suffixGroup := range group.Actions {
			var held []ApiPermission
			for _, code := range suffixGroup {
				if index := slices.Index(remaining, code); index >= 0 {
					remaining = slices.Delete(remaining, index, index+1)
					held = append(held, code)
				}
			}
			if len(held) > 0 {
				kept.Actions = append(kept.Actions, held)
			}
		}
		if len(kept.Actions) > 0 {
			only = append(only, kept)
		}
	}
	if len(remaining) > 0 {
		other := ApiKeyPermissionGroup{Name: "Other"}
		for _, code := range remaining {
			other.Actions = append(other.Actions, []ApiPermission{code})
		}
		only = append(only, other)
	}
	return only
}

func (p ApiKeyPermissions) MarkdownString() string {
	var lines []string
	for _, group := range p {
		lines = append(lines, "  - "+group.Name+": `"+strings.Join(group.Patterns(), "`, `")+"`")
	}
	return "\n" + strings.Join(lines, "\n")
}

// Patterns merges the codes of g into patterns: `[ADM_]TENANT_(DELETE|LIST)` stands for
// TENANT_DELETE, TENANT_LIST, ADM_TENANT_DELETE and ADM_TENANT_LIST, and
// `(MANAGED_|ADM_)TENANT_IMPORT` for MANAGED_TENANT_IMPORT and ADM_TENANT_IMPORT. A pattern never
// stands for a code that is not in g.
func (g ApiKeyPermissionGroup) Patterns() []string {
	var codes []ApiPermission
	for _, suffixGroup := range g.Actions {
		codes = append(codes, suffixGroup...)
	}
	type resourceVerb struct{ resource, verb string }
	var order []resourceVerb
	prefixesOf := map[resourceVerb][]string{}
	for _, code := range codes {
		prefix, rest := "", string(code)
		for _, candidate := range permissionPrefixes[1:] {
			if unprefixed, found := strings.CutPrefix(rest, candidate); found {
				prefix, rest = candidate, unprefixed
				break
			}
		}
		resource, verb, _ := strings.CutLast(rest, "_")
		key := resourceVerb{resource, verb}
		if _, seen := prefixesOf[key]; !seen {
			order = append(order, key)
		}
		prefixesOf[key] = append(prefixesOf[key], prefix)
	}

	var merged []*permissionPattern
	for _, key := range order {
		prefixes := prefixesOf[key]
		slices.SortFunc(prefixes, func(a, b string) int {
			return slices.Index(permissionPrefixes, a) - slices.Index(permissionPrefixes, b)
		})
		// A code without a verb, such as an unknown FOO, would turn FOO_LIST into FOO_(|LIST).
		index := slices.IndexFunc(merged, func(p *permissionPattern) bool {
			return key.verb != "" && p.verbs[0] != "" && p.resource == key.resource && slices.Equal(p.prefixes, prefixes)
		})
		if index < 0 {
			merged = append(merged, &permissionPattern{prefixes: prefixes, resource: key.resource})
			index = len(merged) - 1
		}
		merged[index].verbs = append(merged[index].verbs, key.verb)
	}

	patterns := make([]string, 0, len(merged))
	for _, p := range merged {
		patterns = append(patterns, p.String())
	}
	return patterns
}

// permissionPrefixes are in the order a pattern lists them. The empty prefix of the workspace
// scope stays first, because Patterns and permissionPattern.String rely on its index.
var permissionPrefixes = []string{"", "MANAGED_", "ADM_"}

type permissionPattern struct {
	prefixes []string
	resource string
	verbs    []string
}

func (p permissionPattern) String() string {
	var prefix string
	switch {
	case len(p.prefixes) == 1:
		prefix = p.prefixes[0]
	case p.prefixes[0] == "":
		prefix = "[" + strings.Join(p.prefixes[1:], "|") + "]"
	default:
		prefix = "(" + strings.Join(p.prefixes, "|") + ")"
	}
	switch {
	case p.verbs[0] == "":
		return prefix + p.resource
	case len(p.verbs) == 1:
		return prefix + p.resource + "_" + p.verbs[0]
	default:
		return prefix + p.resource + "_(" + strings.Join(p.verbs, "|") + ")"
	}
}

// Permissions must stay in step with ApiKeyRightMetadataRegistry in the meshStack backend, which
// https://docs.meshcloud.io/api/authentication/api-permissions/ documents.
var Permissions = ApiKeyPermissions{
	{Name: "API Keys", Actions: [][]ApiPermission{
		{"APIKEY_DELETE", "ADM_APIKEY_DELETE"},
		{"APIKEY_LIST", "ADM_APIKEY_LIST"},
		{"APIKEY_SAVE", "ADM_APIKEY_SAVE"},
	}},
	{Name: "Building Blocks", Actions: [][]ApiPermission{
		{"BUILDINGBLOCK_DELETE", "ADM_BUILDINGBLOCK_DELETE"},
		{"BUILDINGBLOCK_LIST", "ADM_BUILDINGBLOCK_LIST", "MANAGED_BUILDINGBLOCK_LIST"},
		{"BUILDINGBLOCK_SAVE", "ADM_BUILDINGBLOCK_SAVE", "MANAGED_BUILDINGBLOCK_SAVE"},
	}},
	{Name: "Building Block Definitions", Actions: [][]ApiPermission{
		{"BUILDINGBLOCKDEFINITION_DELETE", "ADM_BUILDINGBLOCKDEFINITION_DELETE"},
		{"BUILDINGBLOCKDEFINITION_LIST", "ADM_BUILDINGBLOCKDEFINITION_LIST"},
		{"BUILDINGBLOCKDEFINITION_SAVE", "ADM_BUILDINGBLOCKDEFINITION_SAVE"},
		{"ADM_REVIEW_PUBLICATION"},
	}},
	{Name: "Building Block Runs", Actions: [][]ApiPermission{
		{"MANAGED_BUILDINGBLOCKRUN_LIST", "ADM_BUILDINGBLOCKRUN_LIST"},
		{"MANAGED_BUILDINGBLOCKRUN_SAVE", "ADM_BUILDINGBLOCKRUN_SAVE"},
		{"MANAGED_BUILDINGBLOCKRUNSOURCE_SAVE", "ADM_BUILDINGBLOCKRUNSOURCE_SAVE"},
		{"MANAGED_BUILDINGBLOCKRUNARTIFACT_LIST", "ADM_BUILDINGBLOCKRUNARTIFACT_LIST"},
		{"MANAGED_BUILDINGBLOCKRUNARTIFACT_SAVE", "ADM_BUILDINGBLOCKRUNARTIFACT_SAVE"},
	}},
	{Name: "Building Block Runners", Actions: [][]ApiPermission{
		{"BUILDINGBLOCKRUNNER_DELETE", "ADM_BUILDINGBLOCKRUNNER_DELETE"},
		{"BUILDINGBLOCKRUNNER_LIST", "ADM_BUILDINGBLOCKRUNNER_LIST"},
		{"BUILDINGBLOCKRUNNER_SAVE", "ADM_BUILDINGBLOCKRUNNER_SAVE"},
	}},
	{Name: "Communication Definitions", Actions: [][]ApiPermission{
		{"COMMUNICATIONDEFINITION_DELETE", "ADM_COMMUNICATIONDEFINITION_DELETE"},
		{"COMMUNICATIONDEFINITION_LIST", "ADM_COMMUNICATIONDEFINITION_LIST"},
		{"COMMUNICATIONDEFINITION_SAVE", "ADM_COMMUNICATIONDEFINITION_SAVE"},
	}},
	{Name: "Communications", Actions: [][]ApiPermission{
		{"COMMUNICATION_DELETE", "ADM_COMMUNICATION_DELETE"},
		{"COMMUNICATION_LIST", "ADM_COMMUNICATION_LIST"},
		{"COMMUNICATION_SAVE", "ADM_COMMUNICATION_SAVE"},
	}},
	{Name: "Event Logs", Actions: [][]ApiPermission{
		{"EVENTLOG_LIST", "ADM_EVENTLOG_LIST"},
	}},
	{Name: "Integrations", Actions: [][]ApiPermission{
		{"INTEGRATION_DELETE", "ADM_INTEGRATION_DELETE"},
		{"INTEGRATION_LIST", "ADM_INTEGRATION_LIST"},
		{"INTEGRATION_SAVE", "ADM_INTEGRATION_SAVE"},
	}},
	{Name: "Landing Zones", Actions: [][]ApiPermission{
		{"LANDINGZONE_DELETE", "ADM_LANDINGZONE_DELETE"},
		{"LANDINGZONE_LIST", "ADM_LANDINGZONE_LIST"},
		{"LANDINGZONE_SAVE", "ADM_LANDINGZONE_SAVE"},
	}},
	{Name: "Payment Methods", Actions: [][]ApiPermission{
		{"ADM_PAYMENTMETHOD_DELETE"},
		{"PAYMENTMETHOD_LIST", "ADM_PAYMENTMETHOD_LIST"},
		{"ADM_PAYMENTMETHOD_SAVE"},
	}},
	{Name: "Platform Instances, Platform Types, Locations", Actions: [][]ApiPermission{
		{"PLATFORMINSTANCE_DELETE", "ADM_PLATFORMINSTANCE_DELETE"},
		{"PLATFORMINSTANCE_LIST", "ADM_PLATFORMINSTANCE_LIST"},
		{"PLATFORMINSTANCE_SAVE", "ADM_PLATFORMINSTANCE_SAVE"},
	}},
	{Name: "Project Role Bindings", Actions: [][]ApiPermission{
		{"PROJECTPRINCIPALROLE_DELETE", "ADM_PROJECTPRINCIPALROLE_DELETE"},
		{"PROJECTPRINCIPALROLE_LIST", "ADM_PROJECTPRINCIPALROLE_LIST"},
		{"PROJECTPRINCIPALROLE_SAVE", "ADM_PROJECTPRINCIPALROLE_SAVE"},
	}},
	{Name: "Project Roles", Actions: [][]ApiPermission{
		{"ADM_PROJECTROLE_DELETE"},
		{"ADM_PROJECTROLE_SAVE"},
	}},
	{Name: "Projects", Actions: [][]ApiPermission{
		{"PROJECT_DELETE", "ADM_PROJECT_DELETE"},
		{"PROJECT_LIST", "ADM_PROJECT_LIST"},
		{"PROJECT_SAVE", "ADM_PROJECT_SAVE"},
	}},
	{Name: "Service Instances", Actions: [][]ApiPermission{
		{"SERVICEINSTANCE_DELETE", "ADM_SERVICEINSTANCE_DELETE"},
		{"SERVICEINSTANCE_LIST", "ADM_SERVICEINSTANCE_LIST"},
		{"SERVICEINSTANCE_SAVE", "ADM_SERVICEINSTANCE_SAVE"},
	}},
	{Name: "Tag Definitions", Actions: [][]ApiPermission{
		{"ADM_TAGDEFINITION_DELETE"},
		{"ADM_TAGDEFINITION_LIST"},
		{"ADM_TAGDEFINITION_SAVE"},
	}},
	{Name: "Tenant Usage Reports", Actions: [][]ApiPermission{
		{"TENANTUSAGEREPORT_LIST", "ADM_TENANTUSAGEREPORT_LIST"},
	}},
	{Name: "Tenants", Actions: [][]ApiPermission{
		{"TENANT_DELETE", "ADM_TENANT_DELETE"},
		{"MANAGED_TENANT_IMPORT", "ADM_TENANT_IMPORT"},
		{"TENANT_LIST", "ADM_TENANT_LIST"},
		{"TENANT_SAVE", "ADM_TENANT_SAVE"},
		{"MANAGED_TENANTDELETION_APPROVE", "ADM_TENANTDELETION_APPROVE"},
	}},
	{Name: "Terraform States", Actions: [][]ApiPermission{
		{"TFSTATE_DELETE", "ADM_TFSTATE_DELETE", "MANAGED_TFSTATE_DELETE"},
		{"TFSTATE_LIST", "ADM_TFSTATE_LIST", "MANAGED_TFSTATE_LIST"},
		{"TFSTATE_SAVE", "ADM_TFSTATE_SAVE", "MANAGED_TFSTATE_SAVE"},
	}},
	{Name: "Users", Actions: [][]ApiPermission{
		{"ADM_USER_DELETE"},
		{"ADM_USER_LIST"},
		{"ADM_USER_SAVE"},
	}},
	{Name: "Workspace Role Bindings", Actions: [][]ApiPermission{
		{"WORKSPACEPRINCIPALBINDING_DELETE", "ADM_WORKSPACEPRINCIPALBINDING_DELETE"},
		{"WORKSPACEPRINCIPALBINDING_LIST", "ADM_WORKSPACEPRINCIPALBINDING_LIST"},
		{"WORKSPACEPRINCIPALBINDING_SAVE", "ADM_WORKSPACEPRINCIPALBINDING_SAVE"},
	}},
	{Name: "Workspace User Groups", Actions: [][]ApiPermission{
		{"ADM_WORKSPACEUSERGROUP_DELETE"},
		{"WORKSPACEUSERGROUP_LIST", "ADM_WORKSPACEUSERGROUP_LIST"},
		{"ADM_WORKSPACEUSERGROUP_SAVE"},
	}},
	{Name: "Workspaces", Actions: [][]ApiPermission{
		{"WORKSPACE_DELETE", "ADM_WORKSPACE_DELETE"},
		{"WORKSPACE_LIST", "ADM_WORKSPACE_LIST"},
		{"WORKSPACE_SAVE", "ADM_WORKSPACE_SAVE"},
	}},
}

func AllApiKeyPermissions() []string {
	return Permissions.AllCodes()
}

func WorkspacePermissionCodes() []string {
	return Permissions.WorkspaceCodes()
}
