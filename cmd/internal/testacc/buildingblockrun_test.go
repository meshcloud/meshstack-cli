package testacc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// followOfAFinishedRunWritesItsLogsAndEnds follows a run that has already finished, the only kind
// the local stack holds: a run of a manual building block finishes as it starts.
func followOfAFinishedRunWritesItsLogsAndEnds(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		runs, err := c.run("", "buildingblockrun", "list", "--limit", "20", "-o", "ndjson")
		require.NoErrorf(t, err, "the runs could not be listed:\n%s", runs)
		runUuid, status := firstFinishedRun(runs)
		if runUuid == "" {
			t.Skip("the local stack holds no finished building block run to follow")
		}

		logs, err := c.run("", "buildingblockrun", "logs", runUuid, "-o", "ndjson")
		require.NoErrorf(t, err, "the logs could not be read:\n%s", logs)
		followed, err := c.run("", "buildingblockrun", "logs", runUuid, "--follow")

		switch status {
		case "SUCCEEDED":
			require.NoErrorf(t, err, "following a succeeded run failed:\n%s", followed)
		default:
			require.Errorf(t, err, "following a %s run ended in exit status 0:\n%s", status, followed)
			assert.Regexp(t, "building block run "+runUuid+" (failed|was aborted)", err.Error())
		}
		for _, step := range stepsOf(t, logs) {
			assert.Contains(t, followed, step.DisplayName+": "+step.Status+"\n")
		}
	}
}

func firstFinishedRun(ndjson string) (runUuid, status string) {
	for _, run := range ndjsonObjects[struct {
		Metadata struct {
			Uuid string `json:"uuid"`
		} `json:"metadata"`
		Status string `json:"status"`
	}](ndjson) {
		if run.Metadata.Uuid != "" && run.Status != "IN_PROGRESS" {
			return run.Metadata.Uuid, run.Status
		}
	}
	return "", ""
}

type stepLog struct {
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

func stepsOf(t *testing.T, ndjson string) []stepLog {
	t.Helper()
	logs := ndjsonObjects[struct {
		Steps []stepLog `json:"steps"`
	}](ndjson)
	require.NotEmptyf(t, logs, "the logs command wrote no JSON:\n%s", ndjson)
	return logs[0].Steps
}
