package meshstack_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

func TestTheAdminWorkspaceIsLabelledAsSuchInsteadOfByItsDisplayName(t *testing.T) {
	workspaces := meshstack.Workspaces{
		Items: []client.MeshWorkspace{
			{Metadata: client.MeshWorkspaceMetadata{Name: "admin"}, Spec: client.MeshWorkspaceSpec{DisplayName: "Partner"}},
			{Metadata: client.MeshWorkspaceMetadata{Name: "ops"}, Spec: client.MeshWorkspaceSpec{DisplayName: "Operations"}},
		},
		AdminWorkspace: "admin",
	}

	var labels []string
	for _, candidate := range workspaces.All() {
		labels = append(labels, candidate.Label())
	}

	assert.Equal(t, []string{"**ADMIN** (admin)", "Operations (ops)"}, labels)
}

func TestNoWorkspaceIsAdminWhereTheAdminWorkspaceIsUnknown(t *testing.T) {
	workspaces := meshstack.Workspaces{Items: []client.MeshWorkspace{{Metadata: client.MeshWorkspaceMetadata{Name: ""}}}}

	for _, candidate := range workspaces.All() {
		assert.False(t, candidate.IsAdmin)
	}
}
