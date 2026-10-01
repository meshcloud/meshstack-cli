package testacc

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAccEveryListCommandAnswers covers what TestAccEveryGetOperationAnswers does not: a list command
// sends query parameters of its own on top of those the API docs describe, such as the sort that
// keeps its pages stable.
func TestAccEveryListCommandAnswers(t *testing.T) {
	c := loggedInWithApiKey(t)

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
	definition := firstUuid(t, list("buildingblockdefinition", "list"))
	if definition == "" {
		t.Log("the local stack holds no building block definition, so no version list ran")
		return
	}
	list("buildingblockdefinitionversion", "list", "--definition", definition)
}

func firstUuid(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.Lines(output) {
		var object struct {
			Metadata struct {
				Uuid string `json:"uuid"`
			} `json:"metadata"`
		}
		if json.Unmarshal([]byte(line), &object) == nil && object.Metadata.Uuid != "" {
			return object.Metadata.Uuid
		}
	}
	return ""
}
