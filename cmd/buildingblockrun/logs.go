package buildingblockrun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newLogs() *cobra.Command {
	var (
		output  internal.OutputFlag
		follow  bool
		runUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "logs <run-uuid>",
		Short: "Show the logs of a building block run",
		Long: `Show the logs of a building block run.

The run is named by its uuid, which "meshstack buildingblockrun list" reports as metadata.uuid.
"meshstack api-docs --describe buildingblockrun.logs" shows the fields of the answer.

--follow writes what is new as text until the run has finished, and ends with exit status 0 when
the run succeeded and 1 otherwise, so a script can wait on a run with it.`,
		Example: `  meshstack buildingblockrun logs 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c
  meshstack bbrun logs 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c -o ndjson
  meshstack bbrun logs 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c --follow`,
		Args: internal.UuidArg(&runUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			if follow {
				return followRun(ctx, cmd.OutOrStdout(), runUuid, func(ctx context.Context) (runSnapshot, error) {
					return readRun(ctx, meshStack, runUuid)
				})
			}
			logs, err := meshStack.Raw.Get[client.MeshBuildingBlockRun](ctx, runUuid, "logs")
			if err != nil {
				return err
			}
			return output.Format.WriteItem(cmd.OutOrStdout(), logs)
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "write the log as text as it grows, until the run has finished")
	cmd.MarkFlagsMutuallyExclusive("follow", "output")

	return cmd
}

// meshStack reports an aborted run as FAILED, so only the status of its steps can tell that it was
// aborted. MeshBuildingBlockRunRepresentationModelAssembler of ../meshfed-release (meshcloud-internal)
// maps the states.
const (
	runInProgress = "IN_PROGRESS"
	runSucceeded  = "SUCCEEDED"
	runFailed     = "FAILED"
	stepFailed    = "FAILED"
	stepAborted   = "ABORTED"
)

const (
	pollInterval = 2 * time.Second
	maxPollWait  = time.Minute
	// The HTTP client has retried each failed poll for minutes already, see internal/http.
	maxFailedPollsInARow = 5
)

type runSnapshot struct {
	status string
	steps  []client.MeshBuildingBlockRunStepLog
}

// readRun reads the run's status before its logs, so that the logs of a finished run are its
// last ones.
func readRun(ctx context.Context, meshStack client.Client, runUuid uuid.UUID) (runSnapshot, error) {
	rawRun, err := meshStack.Raw.Get[client.MeshBuildingBlockRun](ctx, runUuid)
	if err != nil {
		return runSnapshot{}, err
	}
	var run struct {
		Status string `json:"status"`
	}
	if err = json.Unmarshal(rawRun, &run); err != nil {
		return runSnapshot{}, err
	}
	logs, err := meshStack.BuildingBlockRun.GetLogs(ctx, runUuid.String())
	if err != nil {
		return runSnapshot{}, err
	}
	return runSnapshot{status: run.Status, steps: logs.Steps}, nil
}

func followRun(ctx context.Context, w io.Writer, runUuid uuid.UUID, read func(context.Context) (runSnapshot, error)) error {
	var (
		written      writtenLogs
		readOnce     bool
		failedInARow int
		wait         time.Duration
	)
	for {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return stoppedFollowing(runUuid)
			case <-time.After(wait):
			}
		}
		run, err := read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return stoppedFollowing(runUuid)
			}
			if !readOnce || isClientError(err) {
				return err
			}
			if failedInARow++; failedInARow == maxFailedPollsInARow {
				return fmt.Errorf("stopped following building block run %s after %d failed reads in a row: %w", runUuid, failedInARow, err)
			}
			wait = min(pollInterval<<failedInARow, maxPollWait)
			slog.WarnContext(ctx, "Could not read the building block run, trying again", "run", runUuid, "in", wait, "error", err)
			continue
		}
		readOnce, failedInARow, wait = true, 0, pollInterval
		if err := written.writeNew(w, run.steps); err != nil {
			return err
		}
		switch run.status {
		case runInProgress:
		case runSucceeded:
			return nil
		case runFailed:
			return run.failure(runUuid)
		default:
			return fmt.Errorf("building block run %s has the status %q, which the meshStack CLI does not know, so it stops following the run", runUuid, run.status)
		}
	}
}

func stoppedFollowing(runUuid uuid.UUID) error {
	return fmt.Errorf("stopped following building block run %s before it finished", runUuid)
}

// isClientError tells a refusal that a later poll would meet again, such as a run that was
// deleted, from a failure that can clear.
func isClientError(err error) bool {
	httpErr, ok := errors.AsType[client.HttpError](err)
	return ok && httpErr.IsClientError()
}

func (run runSnapshot) failure(runUuid uuid.UUID) error {
	var abortedStep string
	for _, step := range run.steps {
		switch step.Status {
		case stepFailed:
			return fmt.Errorf("building block run %s failed at step %q", runUuid, step.DisplayName)
		case stepAborted:
			if abortedStep == "" {
				abortedStep = step.DisplayName
			}
		}
	}
	if abortedStep != "" {
		return fmt.Errorf("building block run %s was aborted at step %q", runUuid, abortedStep)
	}
	return fmt.Errorf("building block run %s failed", runUuid)
}

// writtenLogs is what --follow has written of each step, matched to the next read by position,
// because a step in the logs answer has no id.
type writtenLogs []writtenStep

type writtenStep struct {
	displayName   string
	status        string
	statusWritten bool
	userLines     messageLines
	systemLines   messageLines
}

func (written *writtenLogs) writeNew(w io.Writer, steps []client.MeshBuildingBlockRunStepLog) error {
	for i, step := range steps {
		if i == len(*written) {
			*written = append(*written, writtenStep{displayName: step.DisplayName})
		}
		previous := &(*written)[i]
		if previous.displayName != step.DisplayName {
			*previous = writtenStep{displayName: step.DisplayName}
		}
		if !previous.statusWritten || previous.status != step.Status {
			if _, err := fmt.Fprintf(w, "%s: %s\n", step.DisplayName, step.Status); err != nil {
				return err
			}
			previous.status, previous.statusWritten = step.Status, true
		}
		userLines := messageLinesOf(step.UserMessage)
		if err := previous.userLines.writeNew(w, step.DisplayName+" | ", userLines); err != nil {
			return err
		}
		previous.userLines = userLines
		// An admin reads both messages, and meshStack often sets the two to the same text.
		if step.SystemMessage != nil && (step.UserMessage == nil || *step.SystemMessage != *step.UserMessage) {
			systemLines := messageLinesOf(step.SystemMessage)
			if err := previous.systemLines.writeNew(w, step.DisplayName+" (system) | ", systemLines); err != nil {
				return err
			}
			previous.systemLines = systemLines
		}
	}
	return nil
}

type messageLines []string

func messageLinesOf(message *string) (lines messageLines) {
	if message == nil {
		return nil
	}
	for line := range strings.Lines(*message) {
		lines = append(lines, strings.TrimRight(line, "\r\n"))
	}
	return lines
}

// A runner sends a step's whole message on every update, and some replace it rather than append
// to it.
func (written messageLines) writeNew(w io.Writer, prefix string, lines messageLines) error {
	kept := 0
	for kept < len(written) && kept < len(lines) && written[kept] == lines[kept] {
		kept++
	}
	for _, line := range lines[kept:] {
		if _, err := fmt.Fprintf(w, "%s%s\n", prefix, line); err != nil {
			return err
		}
	}
	return nil
}
