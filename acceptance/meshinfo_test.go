package acceptance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccMeshInfo is the shared precheck's own test: the precheck needs /mesh/info only to
// answer, and a backend that answers it without naming a version is broken in a way worth
// failing on by name.
func TestAccMeshInfo(t *testing.T) {
	endpoint, info := requireLocalStack(t)

	assert.NotEmptyf(t, info.Version, "%s/mesh/info reported no version", endpoint)
}
