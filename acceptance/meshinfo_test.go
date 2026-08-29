package acceptance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccMeshInfo is the cheap one, and it is here to fail first: everything else in this
// package needs a backend that is up and answering, and a suite that discovers that from a
// login timing out reads as a bug in the login.
func TestAccMeshInfo(t *testing.T) {
	endpoint := requireLocalStack(t)

	info := meshInfo(t, endpoint)

	assert.NotEmptyf(t, info.Version, "%s/mesh/info reported no version", endpoint)
}
