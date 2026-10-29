// Package buildingblock waits for the run of a building block, by the rules of
// [client.MeshBuildingBlockV2Status.RunChangedSince].
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
	// runStartTimeout is runStartTimeout of terraform-provider-meshstack, which waits for the same:
	// starting a run is a write of meshStack's own, so a meshStack that has not done it within a
	// minute is not going to.
	runStartTimeout      = time.Minute
	runStartPollInterval = time.Second
)

type RunWait struct {
	raw               *client.RawClient
	buildingBlockUuid uuid.UUID
	startEvery        time.Duration
	startWithin       time.Duration
}

func NewRunWait(raw *client.RawClient, buildingBlockUuid uuid.UUID) RunWait {
	return RunWait{raw, buildingBlockUuid, runStartPollInterval, runStartTimeout}
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

func (w RunWait) StartFailure(block *client.MeshBuildingBlockV2) error {
	if block.Status.RunStartFailure == "" {
		return nil
	}
	return withMeshPanelLink(fmt.Errorf("meshStack could not start the run of building block %s: %s", w.buildingBlockUuid, block.Status.RunStartFailure), block)
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
