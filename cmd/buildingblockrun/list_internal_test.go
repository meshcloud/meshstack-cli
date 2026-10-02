package buildingblockrun

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockRuns yields a run named block/i, created at the i-th of createdAts, and counts into pulled the
// runs a reader took from it.
func blockRuns(block string, pulled *int, createdAts ...string) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		for i, createdAt := range createdAts {
			*pulled++
			run := fmt.Sprintf(`{"metadata": {"uuid": "%s/%d", "createdAt": %q}}`, block, i, createdAt)
			if !yield(jsontext.Value(run), nil) {
				return
			}
		}
	}
}

func uuidsOf(t *testing.T, runs iter.Seq2[jsontext.Value, error], n int) []string {
	t.Helper()
	var uuids []string
	for run, err := range runs {
		require.NoError(t, err)
		var listed struct {
			Metadata struct {
				Uuid string `json:"uuid"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(run, &listed))
		if uuids = append(uuids, listed.Metadata.Uuid); len(uuids) == n {
			break
		}
	}
	return uuids
}

func TestMergeNewestFirstListsTheRunsOfEveryBlockNewestFirst(t *testing.T) {
	var pulled int
	merged := mergeNewestFirst([]iter.Seq2[jsontext.Value, error]{
		blockRuns("a", &pulled, "2026-10-01T05:00:00Z", "2026-10-01T03:00:00Z", "2026-10-01T01:00:00Z"),
		blockRuns("empty", &pulled),
		blockRuns("c", &pulled, "2026-10-01T04:00:00Z", "2026-10-01T03:00:00Z", "2026-10-01T00:00:00Z"),
		blockRuns("d", &pulled, "2026-10-01T03:00:00.000Z"),
	})

	assert.Equal(t, []string{"a/0", "c/0", "a/1", "c/1", "d/0", "a/2", "c/2"}, uuidsOf(t, merged, 0),
		"runs created at the same time keep the order of their blocks")
}

func TestMergeNewestFirstReadsOnOnlyInTheBlocksItListed(t *testing.T) {
	var pulledA, pulledB, pulledC int
	merged := mergeNewestFirst([]iter.Seq2[jsontext.Value, error]{
		blockRuns("a", &pulledA, "2026-10-01T05:00:00Z", "2026-10-01T04:00:00Z", "2026-10-01T03:00:00Z"),
		blockRuns("b", &pulledB, "2026-10-01T02:00:00Z", "2026-10-01T01:00:00Z"),
		blockRuns("c", &pulledC, "2026-10-01T01:00:00Z"),
	})

	assert.Equal(t, []string{"a/0", "a/1"}, uuidsOf(t, merged, 2))
	assert.Equal(t, 2, pulledA, "no further than the last listed run of a")
	assert.Equal(t, 1, pulledB, "only the first run of b, before the merge listed any")
	assert.Equal(t, 1, pulledC, "only the first run of c, before the merge listed any")
}

func TestMergeNewestFirstStopsAtTheFirstError(t *testing.T) {
	var pulled int
	failure := errors.New("listing the runs of building block b failed")
	merged := mergeNewestFirst([]iter.Seq2[jsontext.Value, error]{
		blockRuns("a", &pulled, "2026-10-01T05:00:00Z"),
		func(yield func(jsontext.Value, error) bool) { yield(nil, failure) },
	})

	var errs []error
	for _, err := range merged {
		errs = append(errs, err)
	}
	assert.Equal(t, []error{failure}, errs, "no run is listed before every block answered")
}
