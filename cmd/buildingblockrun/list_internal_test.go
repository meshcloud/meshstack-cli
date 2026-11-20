package buildingblockrun

import (
	"errors"
	"fmt"
	"iter"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
)

// blockRuns yields a run named block/i, created at the i-th of createdAts, and counts into pulled the
// runs a reader took from it. The name goes into the first two bytes of the run's uuid.
func blockRuns(t *testing.T, block string, pulled *int, createdAts ...string) iter.Seq2[client.MeshBuildingBlockRun, error] {
	t.Helper()
	return func(yield func(client.MeshBuildingBlockRun, error) bool) {
		for i, createdAt := range createdAts {
			*pulled++
			created, err := time.Parse(time.RFC3339Nano, createdAt)
			require.NoError(t, err)
			id := string([]byte{block[0], byte(i)})
			if !yield(client.MeshBuildingBlockRun{Metadata: client.MeshBuildingBlockRunMetadata{Uuid: id, CreatedAt: created}}, nil) {
				return
			}
		}
	}
}

func namesOf(t *testing.T, runs iter.Seq2[client.MeshBuildingBlockRun, error], n int) []string {
	t.Helper()
	var names []string
	for run, err := range runs {
		require.NoError(t, err)
		if names = append(names, fmt.Sprintf("%c/%d", run.Metadata.Uuid[0], run.Metadata.Uuid[1])); len(names) == n {
			break
		}
	}
	return names
}

func TestMergeNewestFirst(t *testing.T) {
	t.Run("lists the runs of every block newest first", func(t *testing.T) {
		var pulled int
		merged := mergeNewestFirst([]iter.Seq2[client.MeshBuildingBlockRun, error]{
			blockRuns(t, "a", &pulled, "2026-10-01T05:00:00Z", "2026-10-01T03:00:00Z", "2026-10-01T01:00:00Z"),
			blockRuns(t, "empty", &pulled),
			blockRuns(t, "c", &pulled, "2026-10-01T04:00:00Z", "2026-10-01T03:00:00Z", "2026-10-01T00:00:00Z"),
			blockRuns(t, "d", &pulled, "2026-10-01T03:00:00.000Z"),
		})

		assert.Equal(t, []string{"a/0", "c/0", "a/1", "c/1", "d/0", "a/2", "c/2"}, namesOf(t, merged, 0),
			"runs created at the same time keep the order of their blocks")
	})

	t.Run("reads no block further than its first run or the last run it listed", func(t *testing.T) {
		var pulledA, pulledB, pulledC int
		merged := mergeNewestFirst([]iter.Seq2[client.MeshBuildingBlockRun, error]{
			blockRuns(t, "a", &pulledA, "2026-10-01T05:00:00Z", "2026-10-01T04:00:00Z", "2026-10-01T03:00:00Z"),
			blockRuns(t, "b", &pulledB, "2026-10-01T02:00:00Z", "2026-10-01T01:00:00Z"),
			blockRuns(t, "c", &pulledC, "2026-10-01T01:00:00Z"),
		})

		assert.Equal(t, []string{"a/0", "a/1"}, namesOf(t, merged, 2))
		assert.Equal(t, 2, pulledA, "no further than the last listed run of a")
		assert.Equal(t, 1, pulledB, "only the first run of b, before the merge listed any")
		assert.Equal(t, 1, pulledC, "only the first run of c, before the merge listed any")
	})

	t.Run("yields only the error of a block that fails before every block answered", func(t *testing.T) {
		var pulled int
		failure := errors.New("listing the runs of building block b failed")
		merged := mergeNewestFirst([]iter.Seq2[client.MeshBuildingBlockRun, error]{
			blockRuns(t, "a", &pulled, "2026-10-01T05:00:00Z"),
			func(yield func(client.MeshBuildingBlockRun, error) bool) {
				yield(client.MeshBuildingBlockRun{}, failure)
			},
		})

		var errs []error
		for _, err := range merged {
			errs = append(errs, err)
		}
		assert.Equal(t, []error{failure}, errs, "no run is listed before every block answered")
	})
}
