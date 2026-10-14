package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func TestKindCommandTakesTheKindTheCommandOrAnAlias(t *testing.T) {
	for _, name := range []string{"meshBuildingBlockDefinition", "buildingblockdefinition", "BBD"} {
		assert.Equal(t, internal.KindCommand("buildingblockdefinition"), internal.KindCommandOf(name), name)
	}
	assert.Equal(t, internal.KindCommand("landingzone"), internal.KindCommandOf("meshLandingZone"), "a kind without aliases")
}
