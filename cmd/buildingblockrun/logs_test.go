package buildingblockrun

import (
	"context"
	"errors"
	"io"
	gohttp "net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
)

var runUuid = uuid.MustParse("7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c")

func step(displayName, status string, userMessage, systemMessage *string) client.MeshBuildingBlockRunStepLog {
	return client.MeshBuildingBlockRunStepLog{DisplayName: displayName, Status: status, UserMessage: userMessage, SystemMessage: systemMessage}
}

func TestFollowWritesOnlyWhatIsNewAndStartsOverAtAStepThatTookAnotherStepsPlace(t *testing.T) {
	snapshots := []struct {
		steps []client.MeshBuildingBlockRunStepLog
		want  string
	}{
		{
			steps: []client.MeshBuildingBlockRunStepLog{step("Init", "IN_PROGRESS", new("Cloning\n"), nil)},
			want:  "Init: IN_PROGRESS\nInit | Cloning\n",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{step("Init", "IN_PROGRESS", new("Cloning\n"), nil)},
			want:  "",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{step("Init", "IN_PROGRESS", new("Cloning\nCloned"), nil)},
			want:  "Init | Cloned\n",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{
				step("Init", "SUCCEEDED", new("Cloning\nCloned"), new("Cloning\nCloned")),
				step("Pipeline", "PENDING", nil, nil),
			},
			want: "Init: SUCCEEDED\nPipeline: PENDING\n",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{
				step("Init", "SUCCEEDED", new("Cloning\nCloned"), new("Cloning\nCloned")),
				step("Pipeline", "IN_PROGRESS", new("Run 1 is queued"), new("Run 1 is queued, see https://ci")),
			},
			want: "Pipeline: IN_PROGRESS\nPipeline | Run 1 is queued\nPipeline (system) | Run 1 is queued, see https://ci\n",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{
				step("Init", "SUCCEEDED", new("Cloning\nCloned"), new("Cloning\nCloned")),
				step("Pipeline", "IN_PROGRESS", new("Run 1 is running"), new("Run 1 is queued, see https://ci")),
			},
			want: "Pipeline | Run 1 is running\n",
		},
		{
			steps: []client.MeshBuildingBlockRunStepLog{step("Apply", "SUCCEEDED", new("done"), nil)},
			want:  "Apply: SUCCEEDED\nApply | done\n",
		},
	}

	var written writtenLogs
	for i, snapshot := range snapshots {
		var out strings.Builder
		require.NoError(t, written.writeNew(&out, snapshot.steps))
		assert.Equalf(t, snapshot.want, out.String(), "snapshot %d", i)
	}
}

// fakeRun answers each read with the next of its snapshots, and with the last one from then on.
type fakeRun struct {
	answers []fakeAnswer
	reads   int
}

type fakeAnswer struct {
	snapshot runSnapshot
	err      error
}

func (f *fakeRun) read(context.Context) (runSnapshot, error) {
	answer := f.answers[min(f.reads, len(f.answers)-1)]
	f.reads++
	return answer.snapshot, answer.err
}

func inProgress(steps ...client.MeshBuildingBlockRunStepLog) fakeAnswer {
	return fakeAnswer{snapshot: runSnapshot{status: runInProgress, steps: steps}}
}

func finished(status string, steps ...client.MeshBuildingBlockRunStepLog) fakeAnswer {
	return fakeAnswer{snapshot: runSnapshot{status: status, steps: steps}}
}

func TestFollowEndsWithTheRun(t *testing.T) {
	tests := []struct {
		name     string
		finished fakeAnswer
		wantErr  string
	}{
		{"succeeded", finished(runSucceeded, step("Apply", "SUCCEEDED", nil, nil)), ""},
		{
			"failed", finished(runFailed, step("Plan", "SUCCEEDED", nil, nil), step("Apply", "FAILED", nil, nil)),
			`building block run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c failed at step "Apply"`,
		},
		{
			"aborted, which meshStack reports as failed", finished(runFailed, step("Apply", "ABORTED", nil, nil)),
			`building block run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c was aborted at step "Apply"`,
		},
		{"failed with no steps", finished(runFailed), "building block run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c failed"},
		{
			"in a status the CLI does not know", finished("WAITING"),
			`building block run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c has the status "WAITING", which the meshStack CLI does not know, so it stops following the run`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				run := &fakeRun{answers: []fakeAnswer{inProgress(), inProgress(), test.finished}}
				start := time.Now()

				err := followRun(t.Context(), io.Discard, runUuid, run.read)

				if test.wantErr == "" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, test.wantErr)
				}
				assert.Equal(t, 3, run.reads, "it reads until the run has finished, and no further")
				assert.Equal(t, 2*pollInterval, time.Since(start))
			})
		})
	}
}

func TestFollowRetriesAFailedReadWithLongerWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		unavailable := fakeAnswer{err: client.HttpError{StatusCode: gohttp.StatusInternalServerError}}
		run := &fakeRun{answers: []fakeAnswer{
			inProgress(step("Apply", "IN_PROGRESS", new("one"), nil)),
			unavailable, unavailable,
			finished(runSucceeded, step("Apply", "SUCCEEDED", new("one\ntwo"), nil)),
		}}
		var out strings.Builder
		start := time.Now()

		require.NoError(t, followRun(t.Context(), &out, runUuid, run.read))

		assert.Equal(t, "Apply: IN_PROGRESS\nApply | one\nApply: SUCCEEDED\nApply | two\n", out.String())
		assert.Equal(t, pollInterval+2*pollInterval+4*pollInterval, time.Since(start))
	})
}

func TestFollowGivesUpOnAReadThatKeepsFailing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		unreachable := errors.New("connection refused")
		run := &fakeRun{answers: []fakeAnswer{inProgress(), {err: unreachable}}}

		err := followRun(t.Context(), io.Discard, runUuid, run.read)

		require.ErrorIs(t, err, unreachable)
		assert.Equal(t, 1+maxFailedPollsInARow, run.reads)
	})
}

func TestFollowEndsAtOnceOnAReadThatCannotSucceed(t *testing.T) {
	notFound := fakeAnswer{err: client.HttpError{StatusCode: gohttp.StatusNotFound}}
	tests := map[string][]fakeAnswer{
		"the first read fails":    {{err: errors.New("no such host")}},
		"meshStack refuses":       {inProgress(), notFound},
		"meshStack refuses first": {notFound},
	}
	for name, answers := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				run := &fakeRun{answers: answers}

				require.Error(t, followRun(t.Context(), io.Discard, runUuid, run.read))
				assert.Len(t, answers, run.reads)
			})
		})
	}
}

func TestFollowStopsWhenInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, interrupt := context.WithCancel(t.Context())
		run := &fakeRun{answers: []fakeAnswer{inProgress()}}
		done := make(chan error)
		go func() { done <- followRun(ctx, io.Discard, runUuid, run.read) }()

		synctest.Sleep(pollInterval / 2)
		interrupt()

		require.EqualError(t, <-done, "stopped following building block run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c before it finished")
		assert.Equal(t, 1, run.reads)
	})
}
