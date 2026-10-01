package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func TestKindCommandTakesTheKindTheCommandOrAnAlias(t *testing.T) {
	for _, name := range []string{"meshBuildingBlockDefinition", "buildingblockdefinition", "BBD"} {
		assert.Equal(t, "buildingblockdefinition", internal.KindCommand(name), name)
	}
	assert.Equal(t, "landingzone", internal.KindCommand("meshLandingZone"), "a kind without aliases")
}
