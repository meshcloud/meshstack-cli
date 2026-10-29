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
	// runStartTimeout is runStartTimeout of terraform-provider-meshstack, which waits for the same:
	// starting a run is a write of meshStack's own, so a meshStack that has not done it within a
	// minute is not going to.
	runStartTimeout      = time.Minute
	runStartPollInterval = time.Second
)

func AwaitRunStart(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID, accepted client.MeshBuildingBlockV2Status) (RunStart, error) {
	return awaitRunStart(ctx, raw, buildingBlockUuid, accepted, runStartPollInterval, runStartTimeout)
}

// RunStart holds either the run meshStack started or the reason it could not, along with the
// building block as meshStack reported it then.
type RunStart struct {
	Raw     jsontext.Value
	RunUuid string
	Failure string
}

// awaitRunStart polls the building block until it reports something the trigger-run answer did not:
// a new latest run, or a run start failure recorded anew.
func awaitRunStart(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID, accepted client.MeshBuildingBlockV2Status, every, within time.Duration) (RunStart, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, within, fmt.Errorf(
		"meshStack accepted the run of building block %s, but had not started it %s later. `meshstack buildingblockrun list --building-block %s --limit 1` shows the run once it has",
		buildingBlockUuid, within, buildingBlockUuid))
	defer cancel()
	for {
		current, err := raw.Get[client.MeshBuildingBlockV2](ctx, buildingBlockUuid)
		if err != nil && ctx.Err() != nil {
			return RunStart{}, context.Cause(ctx)
		} else if err != nil {
			return RunStart{}, err
		}
		var block struct {
			Status *client.MeshBuildingBlockV2Status `json:"status"`
		}
		if err := json.Unmarshal(current, &block); err != nil {
			return RunStart{}, err
		}
		if status := block.Status; status != nil {
			switch {
			case status.StartedRunSince(accepted):
				return RunStart{Raw: current, RunUuid: status.GetLatestRunUuid()}, nil
			case status.RunStartFailure != "" && status.FailedToStartRunSince(accepted):
				return RunStart{Raw: current, Failure: status.RunStartFailure}, nil
			}
		}
		select {
		case <-ctx.Done():
			return RunStart{}, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}
