package client

import "strings"

type ApiPermission string

// ApiKeyPermissions nests three levels: a group per resource, in it one entry per action, and in
// that the scope variants of the action, such as TENANT_DELETE and ADM_TENANT_DELETE.
type ApiKeyPermissions [][][]ApiPermission

func (p ApiKeyPermissions) AllCodes() []string {
	var codes []string
	for _, group := range p {
		for _, suffixGroup := range group {
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
		for _, suffixGroup := range group {
			for _, code := range suffixGroup {
				if !strings.HasPrefix(string(code), "ADM_") {
					codes = append(codes, string(code))
				}
			}
		}
	}
	return codes
}

// MarkdownString renders one bullet per resource group: the workspace codes, then the MANAGED_
// codes, then the ADM_ codes, joined by " and ".
func (p ApiKeyPermissions) MarkdownString() string {
	var lines []string
	for _, group := range p {
		var workspace, managed, admin []string
		for _, suffixGroup := range group {
			for _, code := range suffixGroup {
				s := string(code)
				switch {
				case strings.HasPrefix(s, "ADM_"):
					admin = append(admin, "`"+s+"`")
				case strings.HasPrefix(s, "MANAGED_"):
					managed = append(managed, "`"+s+"`")
				default:
					workspace = append(workspace, "`"+s+"`")
				}
			}
		}

		var parts []string
		if len(workspace) > 0 {
			parts = append(parts, strings.Join(workspace, "/"))
		}
		if len(managed) > 0 {
			parts = append(parts, strings.Join(managed, "/"))
		}
		if len(admin) > 0 {
			parts = append(parts, strings.Join(admin, "/"))
		}
		lines = append(lines, "  - "+strings.Join(parts, " and "))
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

// Permissions must stay in step with ApiKeyRightMetadataRegistry in the meshStack backend, which
// https://docs.meshcloud.io/api/authentication/api-permissions/ documents.
var Permissions = ApiKeyPermissions{
	{
		{"APIKEY_DELETE", "ADM_APIKEY_DELETE"},
		{"APIKEY_LIST", "ADM_APIKEY_LIST"},
		{"APIKEY_SAVE", "ADM_APIKEY_SAVE"},
	},
	{
		{"BUILDINGBLOCK_DELETE", "ADM_BUILDINGBLOCK_DELETE"},
		{"BUILDINGBLOCK_LIST", "ADM_BUILDINGBLOCK_LIST", "MANAGED_BUILDINGBLOCK_LIST"},
		{"BUILDINGBLOCK_SAVE", "ADM_BUILDINGBLOCK_SAVE", "MANAGED_BUILDINGBLOCK_SAVE"},
	},
	{
		{"BUILDINGBLOCKDEFINITION_DELETE", "ADM_BUILDINGBLOCKDEFINITION_DELETE"},
		{"BUILDINGBLOCKDEFINITION_LIST", "ADM_BUILDINGBLOCKDEFINITION_LIST"},
		{"BUILDINGBLOCKDEFINITION_SAVE", "ADM_BUILDINGBLOCKDEFINITION_SAVE"},
		{"ADM_REVIEW_PUBLICATION"},
	},
	{
		{"MANAGED_BUILDINGBLOCKRUN_LIST", "ADM_BUILDINGBLOCKRUN_LIST"},
		{"MANAGED_BUILDINGBLOCKRUN_SAVE", "ADM_BUILDINGBLOCKRUN_SAVE"},
		{"MANAGED_BUILDINGBLOCKRUNSOURCE_SAVE", "ADM_BUILDINGBLOCKRUNSOURCE_SAVE"},
	},
	{
		{"BUILDINGBLOCKRUNNER_DELETE", "ADM_BUILDINGBLOCKRUNNER_DELETE"},
		{"BUILDINGBLOCKRUNNER_LIST", "ADM_BUILDINGBLOCKRUNNER_LIST"},
		{"BUILDINGBLOCKRUNNER_SAVE", "ADM_BUILDINGBLOCKRUNNER_SAVE"},
	},
	{
		{"COMMUNICATIONDEFINITION_DELETE", "ADM_COMMUNICATIONDEFINITION_DELETE"},
		{"COMMUNICATIONDEFINITION_LIST", "ADM_COMMUNICATIONDEFINITION_LIST"},
		{"COMMUNICATIONDEFINITION_SAVE", "ADM_COMMUNICATIONDEFINITION_SAVE"},
	},
	{
		{"COMMUNICATION_DELETE", "ADM_COMMUNICATION_DELETE"},
		{"COMMUNICATION_LIST", "ADM_COMMUNICATION_LIST"},
		{"COMMUNICATION_SAVE", "ADM_COMMUNICATION_SAVE"},
	},
	{
		{"EVENTLOG_LIST", "ADM_EVENTLOG_LIST"},
	},
	{
		{"INTEGRATION_DELETE", "ADM_INTEGRATION_DELETE"},
		{"INTEGRATION_LIST", "ADM_INTEGRATION_LIST"},
		{"INTEGRATION_SAVE", "ADM_INTEGRATION_SAVE"},
	},
	{
		{"LANDINGZONE_DELETE", "ADM_LANDINGZONE_DELETE"},
		{"LANDINGZONE_LIST", "ADM_LANDINGZONE_LIST"},
		{"LANDINGZONE_SAVE", "ADM_LANDINGZONE_SAVE"},
	},
	{
		{"ADM_PAYMENTMETHOD_DELETE"},
		{"PAYMENTMETHOD_LIST", "ADM_PAYMENTMETHOD_LIST"},
		{"ADM_PAYMENTMETHOD_SAVE"},
	},
	{
		{"PLATFORMINSTANCE_DELETE", "ADM_PLATFORMINSTANCE_DELETE"},
		{"PLATFORMINSTANCE_LIST", "ADM_PLATFORMINSTANCE_LIST"},
		{"PLATFORMINSTANCE_SAVE", "ADM_PLATFORMINSTANCE_SAVE"},
	},
	{
		{"PROJECTPRINCIPALROLE_DELETE", "ADM_PROJECTPRINCIPALROLE_DELETE"},
		{"PROJECTPRINCIPALROLE_LIST", "ADM_PROJECTPRINCIPALROLE_LIST"},
		{"PROJECTPRINCIPALROLE_SAVE", "ADM_PROJECTPRINCIPALROLE_SAVE"},
	},
	{
		{"ADM_PROJECTROLE_DELETE"},
		{"ADM_PROJECTROLE_SAVE"},
	},
	{
		{"PROJECT_DELETE", "ADM_PROJECT_DELETE"},
		{"PROJECT_LIST", "ADM_PROJECT_LIST"},
		{"PROJECT_SAVE", "ADM_PROJECT_SAVE"},
	},
	{
		{"SERVICEINSTANCE_DELETE", "ADM_SERVICEINSTANCE_DELETE"},
		{"SERVICEINSTANCE_LIST", "ADM_SERVICEINSTANCE_LIST"},
		{"SERVICEINSTANCE_SAVE", "ADM_SERVICEINSTANCE_SAVE"},
	},
	{
		{"ADM_TAGDEFINITION_DELETE"},
		{"ADM_TAGDEFINITION_LIST"},
		{"ADM_TAGDEFINITION_SAVE"},
	},
	{
		{"TENANT_DELETE", "ADM_TENANT_DELETE"},
		{"MANAGED_TENANT_IMPORT", "ADM_TENANT_IMPORT"},
		{"TENANT_LIST", "ADM_TENANT_LIST"},
		{"TENANT_SAVE", "ADM_TENANT_SAVE"},
	},
	{
		{"TFSTATE_DELETE", "ADM_TFSTATE_DELETE", "MANAGED_TFSTATE_DELETE"},
		{"TFSTATE_LIST", "ADM_TFSTATE_LIST", "MANAGED_TFSTATE_LIST"},
		{"TFSTATE_SAVE", "ADM_TFSTATE_SAVE", "MANAGED_TFSTATE_SAVE"},
	},
	{
		{"ADM_USER_DELETE"},
		{"ADM_USER_LIST"},
		{"ADM_USER_SAVE"},
	},
	{
		{"WORKSPACEPRINCIPALBINDING_DELETE", "ADM_WORKSPACEPRINCIPALBINDING_DELETE"},
		{"WORKSPACEPRINCIPALBINDING_LIST", "ADM_WORKSPACEPRINCIPALBINDING_LIST"},
		{"WORKSPACEPRINCIPALBINDING_SAVE", "ADM_WORKSPACEPRINCIPALBINDING_SAVE"},
	},
	{
		{"WORKSPACEUSERGROUP_LIST", "ADM_WORKSPACEUSERGROUP_LIST"},
	},
	{
		{"WORKSPACE_DELETE", "ADM_WORKSPACE_DELETE"},
		{"WORKSPACE_LIST", "ADM_WORKSPACE_LIST"},
		{"WORKSPACE_SAVE", "ADM_WORKSPACE_SAVE"},
	},
}

func AllApiKeyPermissions() []string {
	return Permissions.AllCodes()
}

func WorkspacePermissionCodes() []string {
	return Permissions.WorkspaceCodes()
}
