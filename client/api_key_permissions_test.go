package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermissions(t *testing.T) {
	t.Run("every group has a unique name, and every code is in one group", func(t *testing.T) {
		names := map[string]bool{}
		groupOf := map[ApiPermission]string{}
		for _, group := range Permissions {
			assert.NotEmpty(t, group.Name)
			assert.False(t, names[group.Name], "duplicate name %q", group.Name)
			names[group.Name] = true
			for _, suffixGroup := range group.Actions {
				for _, code := range suffixGroup {
					assert.NotContains(t, groupOf, code, "%s is in %q and %q", code, groupOf[code], group.Name)
					groupOf[code] = group.Name
				}
			}
		}
	})

	t.Run("Only keeps the held codes in their groups, and the unknown ones in Other", func(t *testing.T) {
		held := Permissions.Only([]ApiPermission{
			"WORKSPACE_LIST", "NOT_A_KNOWN_CODE", "TENANT_DELETE", "ADM_TENANT_IMPORT", "MANAGED_TENANT_IMPORT", "ADM_TENANT_DELETE",
		})

		assert.Equal(t, ApiKeyPermissions{
			{Name: "Tenants", Actions: [][]ApiPermission{{"TENANT_DELETE", "ADM_TENANT_DELETE"}, {"MANAGED_TENANT_IMPORT", "ADM_TENANT_IMPORT"}}},
			{Name: "Workspaces", Actions: [][]ApiPermission{{"WORKSPACE_LIST"}}},
			{Name: "Other", Actions: [][]ApiPermission{{"NOT_A_KNOWN_CODE"}}},
		}, held)
		assert.Empty(t, Permissions.Only(nil))
	})

	t.Run("MarkdownString merges the codes into patterns that stand for no other code", func(t *testing.T) {
		permissions := ApiKeyPermissions{
			{Name: "Tenants", Actions: [][]ApiPermission{
				{"TENANT_DELETE", "ADM_TENANT_DELETE"},
				{"MANAGED_TENANT_IMPORT", "ADM_TENANT_IMPORT"},
				{"TENANT_LIST", "ADM_TENANT_LIST"},
				{"TENANT_SAVE"},
			}},
			{Name: "Terraform States", Actions: [][]ApiPermission{{"TFSTATE_LIST", "ADM_TFSTATE_LIST", "MANAGED_TFSTATE_LIST"}}},
			{Name: "Other", Actions: [][]ApiPermission{{"FOO"}, {"FOO_LIST"}}},
		}

		assert.Equal(t, "\n"+
			"  - Tenants: `[ADM_]TENANT_(DELETE|LIST)`, `(MANAGED_|ADM_)TENANT_IMPORT`, `TENANT_SAVE`\n"+
			"  - Terraform States: `[MANAGED_|ADM_]TFSTATE_LIST`\n"+
			"  - Other: `FOO`, `FOO_LIST`", permissions.MarkdownString())
	})
}
