package client

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeshIntegrationConfig_MarshalJSON(t *testing.T) {
	t.Run("without a variant reports the error", func(t *testing.T) {
		_, err := json.Marshal(MeshIntegrationConfig{})
		require.ErrorContains(t, err, "cannot infer integration config type: no variant is set")
	})
}
