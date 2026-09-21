package client

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMeshBuildingBlockDefinitionImplementation_InferType(t *testing.T) {
	t.Run("an empty manual struct counts as set", func(t *testing.T) {
		got, err := MeshBuildingBlockDefinitionImplementation{Manual: &MeshBuildingBlockDefinitionManualImplementation{}}.InferType()
		require.NoError(t, err)
		assert.Equal(t, MeshBuildingBlockImplementationTypeManual, got)
	})

	t.Run("no variant", func(t *testing.T) {
		_, err := MeshBuildingBlockDefinitionImplementation{}.InferType()
		require.EqualError(t, err, "cannot infer implementation type: no variant is set")
	})

	t.Run("several variants", func(t *testing.T) {
		_, err := MeshBuildingBlockDefinitionImplementation{
			Terraform:      &MeshBuildingBlockDefinitionTerraformImplementation{},
			GitlabPipeline: &MeshBuildingBlockDefinitionGitLabPipelineImplementation{},
		}.InferType()
		require.EqualError(t, err, "cannot infer implementation type: more than one variant is set: terraform and gitlabPipeline")
	})
}

func TestMeshIntegrationConfig_InferType(t *testing.T) {
	t.Run("one variant", func(t *testing.T) {
		got, err := MeshIntegrationConfig{Gitlab: &MeshIntegrationGitlabConfig{}}.InferType()
		require.NoError(t, err)
		assert.Equal(t, MeshIntegrationConfigTypeGitlab, got)
	})

	t.Run("no variant", func(t *testing.T) {
		_, err := MeshIntegrationConfig{}.InferType()
		require.EqualError(t, err, "cannot infer integration config type: no variant is set")
	})

	t.Run("several variants", func(t *testing.T) {
		_, err := MeshIntegrationConfig{Github: &MeshIntegrationGithubConfig{}, EntraId: &MeshIntegrationEntraIdConfig{}}.InferType()
		require.EqualError(t, err, "cannot infer integration config type: more than one variant is set: github and entraid")
	})

	t.Run("marshalling without a variant reports the error", func(t *testing.T) {
		_, err := json.Marshal(MeshIntegrationConfig{})
		require.ErrorContains(t, err, "cannot infer integration config type")
	})
}
