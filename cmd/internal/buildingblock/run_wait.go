// Package buildingblock waits for the run of a building block, by the rules of
// [client.MeshBuildingBlockV2Status.RunChangedSince] and [client.MeshBuildingBlockV2.RunEndedSince].
package buildingblock

import (
	"context"
	"fmt"
	"time"

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
	blocks            client.MeshBuildingBlockV2Client
	buildingBlockUuid string
	startEvery        time.Duration
	startWithin       time.Duration
	endEvery          time.Duration
}

func NewRunWait(blocks client.MeshBuildingBlockV2Client, buildingBlockUuid string) RunWait {
	return RunWait{blocks, buildingBlockUuid, runStartPollInterval, runStartTimeout, runEndPollInterval}
}

// AwaitReaction returns the building block once meshStack has reacted to a write since before.
func (w RunWait) AwaitReaction(ctx context.Context, before client.MeshBuildingBlockV2Status) (*client.MeshBuildingBlockV2, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, w.startWithin, fmt.Errorf(
		"meshStack did not react to the write of building block %s within %s: it started no run and reported no failure to start one",
		w.buildingBlockUuid, w.startWithin))
	defer cancel()
	block, err := w.poll(ctx, w.startEvery, func(block *client.MeshBuildingBlockV2) (bool, error) {
		return block.Status != nil && block.Status.RunChangedSince(before), nil
	})
	return block, withMeshPanelLink(err, block)
}

// AwaitEnd returns the building block once meshStack has reacted to a write since before, and the run
// is no longer running.
func (w RunWait) AwaitEnd(ctx context.Context, before client.MeshBuildingBlockV2Status) (*client.MeshBuildingBlockV2, error) {
	if _, err := w.AwaitReaction(ctx, before); err != nil {
		return nil, err
	}
	block, err := w.poll(ctx, w.endEvery, func(block *client.MeshBuildingBlockV2) (bool, error) {
		return block.RunEndedSince(client.MeshBuildingBlockV2Status{})
	})
	return block, withMeshPanelLink(err, block)
}

func (w RunWait) StartFailure(block *client.MeshBuildingBlockV2) error {
	if block.Status.RunStartFailure == "" {
		return nil
	}
	return withMeshPanelLink(fmt.Errorf("meshStack could not start the run of building block %s: %s", w.buildingBlockUuid, block.Status.RunStartFailure), block)
}

// Outcome is an error for a run that ended in a state someone has to act on.
func (w RunWait) Outcome(block *client.MeshBuildingBlockV2) error {
	status := block.Status
	var err error
	switch {
	case status.RunStartFailure != "":
		return w.StartFailure(block)
	case status.RunFailed():
		err = fmt.Errorf("the run of building block %s ended with action %s", w.buildingBlockUuid, status.Action)
	case status.IsWaitingForInput():
		err = fmt.Errorf("the run of building block %s is paused at action %s and continues only after someone acts on it", w.buildingBlockUuid, status.Action)
	}
	return withMeshPanelLink(err, block)
}

func withMeshPanelLink(err error, block *client.MeshBuildingBlockV2) error {
	if err == nil || block == nil {
		return err
	}
	if meshPanel, found := block.Links["meshPanel"]; found {
		return fmt.Errorf("%w. Open it in meshPanel: %s", err, meshPanel.Href)
	}
	return err
}

// poll reads the building block every interval until done says so, and returns the last read, also
// when ctx ends the poll.
func (w RunWait) poll(ctx context.Context, every time.Duration, done func(*client.MeshBuildingBlockV2) (bool, error)) (*client.MeshBuildingBlockV2, error) {
	var last *client.MeshBuildingBlockV2
	for {
		current, err := w.blocks.Read(ctx, w.buildingBlockUuid)
		switch {
		case err != nil && ctx.Err() != nil:
			return last, context.Cause(ctx)
		case err != nil:
			return last, err
		case current == nil:
			return last, fmt.Errorf("building block %s not found", w.buildingBlockUuid)
		}
		last = current
		if ended, err := done(last); ended || err != nil {
			return last, err
		}
		select {
		case <-ctx.Done():
			return last, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}
