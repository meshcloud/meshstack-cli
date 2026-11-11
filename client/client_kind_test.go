package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKind(t *testing.T) {
	assert.Equal(t, inferKind[MeshApiKey](), MeshObjectKind.ApiKey)
	assert.Equal(t, inferKind[MeshBuildingBlockV2](), MeshObjectKind.BuildingBlock)
	assert.Equal(t, inferKind[MeshBuildingBlockDefinition](), MeshObjectKind.BuildingBlockDefinition)
	assert.Equal(t, inferKind[MeshBuildingBlockDefinitionVersion](), MeshObjectKind.BuildingBlockDefinitionVersion)
	assert.Equal(t, inferKind[MeshEventLog](), MeshObjectKind.EventLog)
	assert.Equal(t, inferKind[MeshIntegration](), MeshObjectKind.Integration)
	assert.Equal(t, inferKind[MeshLandingZone](), MeshObjectKind.LandingZone)
	assert.Equal(t, inferKind[MeshLocation](), MeshObjectKind.Location)
	assert.Equal(t, inferKind[MeshPaymentMethod](), MeshObjectKind.PaymentMethod)
	assert.Equal(t, inferKind[MeshPlatform](), MeshObjectKind.Platform)
	assert.Equal(t, inferKind[MeshPlatformType](), MeshObjectKind.PlatformType)
	assert.Equal(t, inferKind[MeshProject](), MeshObjectKind.Project)
	assert.Equal(t, inferKind[MeshProjectGroupBinding](), MeshObjectKind.ProjectGroupBinding)
	assert.Equal(t, inferKind[MeshProjectUserBinding](), MeshObjectKind.ProjectUserBinding)
	assert.Equal(t, inferKind[MeshServiceInstance](), MeshObjectKind.ServiceInstance)
	assert.Equal(t, inferKind[MeshTagDefinition](), MeshObjectKind.TagDefinition)
	assert.Equal(t, inferKind[MeshTenant](), MeshObjectKind.Tenant)
	assert.Equal(t, inferKind[MeshWorkspace](), MeshObjectKind.Workspace)
	assert.Equal(t, inferKind[MeshWorkspaceGroupBinding](), MeshObjectKind.WorkspaceGroupBinding)
	assert.Equal(t, inferKind[MeshWorkspaceUserBinding](), MeshObjectKind.WorkspaceUserBinding)
}
