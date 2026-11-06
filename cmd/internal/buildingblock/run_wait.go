// Package buildingblock waits for the run of a building block.
package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
)

const (
	pendingPollInterval    = time.Second
	inProgressPollInterval = 10 * time.Second
)

type RunWait struct {
	raw               *client.RawClient
	buildingBlockUuid uuid.UUID
	pendingEvery      time.Duration
	inProgressEvery   time.Duration
}

func NewRunWait(raw *client.RawClient, buildingBlockUuid uuid.UUID) RunWait {
	return RunWait{raw, buildingBlockUuid, pendingPollInterval, inProgressPollInterval}
}

// AwaitStart returns the building block once meshStack has started the run, or decided that none starts.
func (w RunWait) AwaitStart(ctx context.Context) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
	started, block, err := w.poll(ctx, func(status client.MeshBuildingBlockV2Status) bool {
		return status.Action != client.BuildingBlockActionPending
	})
	return started, block, withMeshPanelLink(err, block)
}

func (w RunWait) AwaitEnd(ctx context.Context) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
	ended, block, err := w.poll(ctx, func(status client.MeshBuildingBlockV2Status) bool {
		return !status.IsRunning()
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
func (w RunWait) Outcome(block *client.MeshBuildingBlockV2) error {
	status := block.Status
	var err error
	switch {
	case status.RunStartFailure != "":
		return w.StartFailure(block)
	case status.RunFailed():
		err = fmt.Errorf("the run of building block %s ended with action %s", w.buildingBlockUuid, status.Action)
	case status.IsWaitingForInput():
		err = fmt.Errorf("the run of building block %s waits with action %s, and goes on only once someone acts on it", w.buildingBlockUuid, status.Action)
	}
	return withMeshPanelLink(err, block)
}

func withMeshPanelLink(err error, block *client.MeshBuildingBlockV2) error {
	if link := block.MeshPanelLink(); err != nil && link != "" {
		return fmt.Errorf("%w. Open it in meshPanel: %s", err, link)
	}
	return err
}

// poll returns the last read also when ctx ends the poll.
func (w RunWait) poll(ctx context.Context, done func(client.MeshBuildingBlockV2Status) bool) (jsontext.Value, *client.MeshBuildingBlockV2, error) {
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
		every := w.inProgressEvery
		if block.Status != nil {
			if done(*block.Status) {
				return current, last, nil
			}
			if block.Status.Action == client.BuildingBlockActionPending {
				every = w.pendingEvery
			}
		}
		select {
		case <-ctx.Done():
			return nil, last, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}
