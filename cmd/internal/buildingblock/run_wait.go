// Package buildingblock waits for the run of a building block, by the rules of
// [client.MeshBuildingBlockV2Status.RunChangedSince] and [client.MeshBuildingBlockV2.RunEndedSince].
package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
)

const (
	// runStartTimeout is runStartTimeout of terraform-provider-meshstack, which waits for the same:
	// starting a run is a write of meshStack's own, so a meshStack that has not done it within a
	// minute is not going to.
	runStartTimeout      = time.Minute
	runStartPollInterval = time.Second
	runEndPollInterval   = 10 * time.Second
)

type RunWait struct {
	raw               *client.RawClient
	buildingBlockUuid uuid.UUID
	startEvery        time.Duration
	startWithin       time.Duration
	endEvery          time.Duration
}

func NewRunWait(raw *client.RawClient, buildingBlockUuid uuid.UUID) RunWait {
	return RunWait{raw, buildingBlockUuid, runStartPollInterval, runStartTimeout, runEndPollInterval}
}

// AwaitReaction returns the building block once meshStack has reacted to a write since before.
func (w RunWait) AwaitReaction(ctx context.Context, before client.MeshBuildingBlockV2Status) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, w.startWithin, fmt.Errorf(
		"meshStack did not react to the write of building block %s within %s: it started no run and reported no failure to start one",
		w.buildingBlockUuid, w.startWithin))
	defer cancel()
	reacted, block, err := w.poll(ctx, w.startEvery, func(block *client.MeshBuildingBlockV2) (bool, error) {
		return block.Status != nil && block.Status.RunChangedSince(before), nil
	})
	return reacted, block, withMeshPanelLink(err, block)
}

// AwaitEnd returns the building block once its run is no longer running. Given a before, it first
// waits until meshStack has reacted to a write since before; a nil before is the run going on now.
func (w RunWait) AwaitEnd(ctx context.Context, before *client.MeshBuildingBlockV2Status) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
	if before != nil {
		if _, _, err := w.AwaitReaction(ctx, *before); err != nil {
			return nil, nil, err
		}
	}
	ended, block, err := w.poll(ctx, w.endEvery, func(block *client.MeshBuildingBlockV2) (bool, error) {
		return block.RunEndedSince(client.MeshBuildingBlockV2Status{})
	})
	return ended, block, withMeshPanelLink(err, block)
}

func (w RunWait) StartFailure(block *client.MeshBuildingBlockV2) error {
	if block.Status.RunStartFailure == "" {
		return nil
	}
	return withMeshPanelLink(fmt.Errorf("meshStack could not start the run of building block %s: %s", w.buildingBlockUuid, block.Status.RunStartFailure), block)
}

// Outcome is an error for a run that ended in a state someone has to act on.
func (w RunWait) Outcome(ctx context.Context, block *client.MeshBuildingBlockV2) error {
	status := block.Status
	var err error
	switch {
	case status.RunStartFailure != "":
		return w.StartFailure(block)
	case status.RunFailed():
		err = fmt.Errorf("the run of building block %s ended with action %s%s", w.buildingBlockUuid, status.Action, w.waitingBlocks(ctx, status.GetLatestRunUuid()))
	case status.Action == client.BuildingBlockActionApproveRun:
		err = fmt.Errorf("run %s of building block %s is waiting for approval of the plan from the detect run %s. Review the plan with 'meshstack buildingblockrun plan %s'",
			status.GetLatestRunUuid(), w.buildingBlockUuid, status.GetLatestDryRunUuid(), status.GetLatestRunUuid())
	case status.IsWaitingForInput():
		err = fmt.Errorf("the run of building block %s is paused at action %s and continues only after someone acts on it", w.buildingBlockUuid, status.Action)
	}
	return withMeshPanelLink(err, block)
}

// maxMentionedBlocks bounds the reads of waitingBlocks, which reads every uuid in the logs.
const maxMentionedBlocks = 20

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// waitingBlocks names the building blocks that the logs of a failed run mention and that wait for
// someone. A run that writes building blocks, such as a starterkit, fails when one of them waits for
// an approval, and only the run's messages name that building block.
func (w RunWait) waitingBlocks(ctx context.Context, runUuid string) string {
	run, err := uuid.Parse(runUuid)
	if err != nil {
		return ""
	}
	logs, err := w.raw.Get[client.MeshBuildingBlockRun](ctx, run, "logs")
	if err != nil {
		return ""
	}
	var mentioned, waiting []string
	for _, found := range uuidPattern.FindAllString(string(logs), -1) {
		if found == w.buildingBlockUuid.String() || slices.Contains(mentioned, found) || len(mentioned) == maxMentionedBlocks {
			continue
		}
		mentioned = append(mentioned, found)
		read, err := w.raw.Get[client.MeshBuildingBlockV2](ctx, uuid.MustParse(found))
		var block client.MeshBuildingBlockV2
		if err != nil || json.Unmarshal(read, &block) != nil || !block.IsWaitingForInput() {
			continue
		}
		if block.Status.Action == client.BuildingBlockActionApproveRun {
			waiting = append(waiting, fmt.Sprintf("building block %s waits for approval: review its plan with 'meshstack buildingblockrun plan %s'",
				found, block.Status.GetLatestRunUuid()))
		} else {
			waiting = append(waiting, fmt.Sprintf("building block %s waits with action %s", found, block.Status.Action))
		}
	}
	if len(waiting) == 0 {
		return ""
	}
	return fmt.Sprintf(". The logs of the run name building blocks that wait for someone: %s. Act on them, and then run building block %s again",
		strings.Join(waiting, "; "), w.buildingBlockUuid)
}

func withMeshPanelLink(err error, block *client.MeshBuildingBlockV2) error {
	if link := block.MeshPanelLink(); err != nil && link != "" {
		return fmt.Errorf("%w. Open it in meshPanel: %s", err, link)
	}
	return err
}

// poll reads the building block every interval until done says so, and returns the last read, also
// when ctx ends the poll.
func (w RunWait) poll(ctx context.Context, every time.Duration, done func(*client.MeshBuildingBlockV2) (bool, error)) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
	var last *client.MeshBuildingBlockV2
	for {
		current, err := w.raw.Get[client.MeshBuildingBlockV2](ctx, w.buildingBlockUuid)
		if err != nil && ctx.Err() != nil {
			return nil, last, context.Cause(ctx)
		} else if err != nil {
			return nil, last, err
		}
		var block client.MeshBuildingBlockV2
		if err := json.Unmarshal(current, &block); err != nil {
			return nil, last, err
		}
		last = &block
		if ended, err := done(last); ended || err != nil {
			return current, last, err
		}
		select {
		case <-ctx.Done():
			return nil, last, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}
