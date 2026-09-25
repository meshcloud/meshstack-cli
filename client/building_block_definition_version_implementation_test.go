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

	t.Run("marshalling without a variant reports the error", func(t *testing.T) {
		_, err := json.Marshal(MeshBuildingBlockDefinitionImplementation{})
		require.ErrorContains(t, err, "cannot infer implementation type: no variant is set")
	})
}
