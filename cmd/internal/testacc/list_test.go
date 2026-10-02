package testacc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// everyListCommandAnswers covers what everyGetOperationAnswers does not: a list command sends query
// parameters of its own on top of those the API docs describe, such as the sort that keeps its pages
// stable.
func everyListCommandAnswers(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		list := func(args ...string) string {
			t.Helper()
			output, err := c.run("", append(args, "--limit", "1", "-o", "ndjson")...)
			assert.NoErrorf(t, err, "meshstack %s failed:\n%s", strings.Join(args, " "), output)
			return output
		}
		list("workspace", "list")
		list("buildingblock", "list")
		list("buildingblockrun", "list")
		list("eventlog", "list")
		definition := firstUuid(list("buildingblockdefinition", "list"))
		if definition == "" {
			t.Log("the local stack holds no building block definition, so no version list ran")
			return
		}
		list("buildingblockdefinitionversion", "list", "--definition", definition)
	}
}

func firstUuid(output string) string {
	for _, object := range ndjsonObjects[struct {
		Metadata struct {
			Uuid string `json:"uuid"`
		} `json:"metadata"`
	}](output) {
		if object.Metadata.Uuid != "" {
			return object.Metadata.Uuid
		}
	}
	return ""
}
