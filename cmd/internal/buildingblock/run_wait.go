// Package buildingblock waits for the run of a building block.
package buildingblock

import (
	"context"
	"fmt"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
)

const (
	pendingPollInterval    = time.Second
	inProgressPollInterval = 10 * time.Second
)

type RunWait struct {
	blocks            client.MeshBuildingBlockV2Client
	buildingBlockUuid string
	pendingEvery      time.Duration
	inProgressEvery   time.Duration
}

func NewRunWait(blocks client.MeshBuildingBlockV2Client, buildingBlockUuid string) RunWait {
	return RunWait{blocks, buildingBlockUuid, pendingPollInterval, inProgressPollInterval}
}

// AwaitStart returns the building block once meshStack has started the run, or decided that none starts.
func (w RunWait) AwaitStart(ctx context.Context) (*client.MeshBuildingBlockV2, error) {
	block, err := w.poll(ctx, func(status client.MeshBuildingBlockV2Status) bool {
		return status.Action != client.BuildingBlockActionPending
	})
	return block, withMeshPanelLink(err, block)
}

func (w RunWait) AwaitEnd(ctx context.Context) (*client.MeshBuildingBlockV2, error) {
	block, err := w.poll(ctx, func(status client.MeshBuildingBlockV2Status) bool {
		return !status.IsRunning()
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

// poll returns the last read also when ctx ends the poll.
func (w RunWait) poll(ctx context.Context, done func(client.MeshBuildingBlockV2Status) bool) (*client.MeshBuildingBlockV2, error) {
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
		every := w.inProgressEvery
		if current.Status != nil {
			if done(*current.Status) {
				return last, nil
			}
			if current.Status.Action == client.BuildingBlockActionPending {
				every = w.pendingEvery
			}
		}
		select {
		case <-ctx.Done():
			return last, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}
