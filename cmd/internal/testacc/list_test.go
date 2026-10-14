package testacc

import (
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// workspaceHoldingABuildingBlock is for the steps that need a building block, which a listing
// finds in the session's workspace only. It is empty where no workspace holds one.
func (c *cli) workspaceHoldingABuildingBlock(t *testing.T) string {
	t.Helper()
	output, err := c.run("", "workspace", "list", "--limit", "unlimited", "-o", "ndjson")
	require.NoErrorf(t, err, "the workspace list failed:\n%s", output)
	for _, workspace := range ndjsonObjects[struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}](output) {
		in := *c
		in.extraEnv = slices.Clone(c.extraEnv)
		in.setEnv(envWorkspace, workspace.Metadata.Name)
		blocks, err := in.run("", "buildingblock", "list", "--limit", "1", "-o", "ndjson")
		require.NoErrorf(t, err, "the building block list of %s failed:\n%s", workspace.Metadata.Name, blocks)
		if firstUuid(blocks) != "" {
			return workspace.Metadata.Name
		}
	}
	return ""
}

// listingKeepsToTheDefaultWorkspace needs a login that stored a default workspace, as a login that
// selects one does.
func listingKeepsToTheDefaultWorkspace(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		profiles, err := os.ReadFile(c.profilesJson())
		require.NoError(t, err)
		var stored struct {
			Profiles map[string]struct {
				DefaultWorkspace string `json:"default_workspace"`
			} `json:"profiles"`
		}
		require.NoError(t, json.Unmarshal(profiles, &stored))
		defaultWorkspace := stored.Profiles[defaultProfile].DefaultWorkspace
		require.NotEmptyf(t, defaultWorkspace, "the login stored no default workspace:\n%s", profiles)

		output, err := c.run("", "buildingblock", "list", "--limit", "unlimited", "-o", "ndjson")
		require.NoErrorf(t, err, "the building block list failed:\n%s", output)
		for _, block := range ndjsonObjects[struct {
			Metadata struct {
				OwnedByWorkspace string `json:"ownedByWorkspace"`
			} `json:"metadata"`
		}](output) {
			assert.Equal(t, defaultWorkspace, block.Metadata.OwnedByWorkspace, "the list holds a building block of another workspace")
		}
	}
}

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
