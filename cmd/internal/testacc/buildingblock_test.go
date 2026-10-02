package testacc

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ndjsonObjects skips the lines of the log, which share the output with stdout.
func ndjsonObjects[T any](output string) []T {
	var objects []T
	for line := range strings.Lines(output) {
		var object T
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &object) == nil {
			objects = append(objects, object)
		}
	}
	return objects
}

type listedRun struct {
	Metadata struct {
		Uuid string `json:"uuid"`
	} `json:"metadata"`
}

func TestAccTriggerRunNamesTheRunItStarted(t *testing.T) {
	c := loggedInWithApiKey(t)
	output, err := c.run("", "buildingblock", "list", "--limit", "1", "-o", "ndjson")
	require.NoErrorf(t, err, "the building block list failed:\n%s", output)
	buildingBlock := firstUuid(output)
	if buildingBlock == "" {
		t.Skip("the local stack holds no building block to run")
	}

	output, err = c.run("", "buildingblock", "trigger-run", buildingBlock, "-o", "ndjson")
	require.NoErrorf(t, err, "the trigger-run failed:\n%s", output)

	written := ndjsonObjects[struct {
		Status struct {
			LatestRunUuid string `json:"latestRunUuid"`
		} `json:"status"`
	}](output)
	require.Lenf(t, written, 1, "the trigger-run wrote no building block:\n%s", output)
	started := written[0].Status.LatestRunUuid
	assert.Contains(t, output, "meshstack buildingblockrun logs "+started)

	output, err = c.run("", "buildingblockrun", "list", "--building-block", buildingBlock, "--limit", "1", "-o", "ndjson")
	require.NoErrorf(t, err, "the run list failed:\n%s", output)
	runs := ndjsonObjects[listedRun](output)
	require.Lenf(t, runs, 1, "the run list listed no run:\n%s", output)
	assert.Equal(t, started, runs[0].Metadata.Uuid, "the run the trigger-run named is the newest run of the building block")
}
