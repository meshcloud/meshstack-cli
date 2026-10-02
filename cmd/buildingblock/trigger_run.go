package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const (
	// runStartTimeout is runStartTimeout of terraform-provider-meshstack, which waits for the same:
	// starting a run is a write of meshStack's own, so a meshStack that has not done it within a
	// minute is not going to.
	runStartTimeout      = time.Minute
	runStartPollInterval = time.Second
)

func newTriggerRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block, named by its uuid, which "meshstack buildingblock list"
reports as metadata.uuid.

The command waits until meshStack has started the run, and writes the building block as it is
then, whose status.latestRunUuid names the run. It does not wait for the run to finish:
"meshstack buildingblockrun logs <run-uuid>" shows how far it got. When meshStack could not start
the run, the command fails with the reason it gives, status.runStartFailure.
"meshstack api-docs --describe buildingblock.trigger-run" shows the fields of the answer.`,
		Example: `  meshstack buildingblock trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 -o ndjson | jq -r .status.latestRunUuid`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			accepted, err := meshStack.BuildingBlockV2.TriggerRun(ctx, buildingBlockUuid.String())
			if err != nil {
				return err
			}
			started, err := awaitRunStart(ctx, meshStack.Raw, buildingBlockUuid, accepted.Status, runStartPollInterval, runStartTimeout)
			if err != nil {
				return err
			}
			if err := internal.WriteItem(cmd.OutOrStdout(), output.Format, started.raw); err != nil {
				return err
			}
			if started.failure != "" {
				return errors.New(started.failure)
			}
			slog.InfoContext(ctx, fmt.Sprintf("meshStack started run %s of building block %s. Follow it with `meshstack buildingblockrun logs %s`",
				started.runUuid, buildingBlockUuid, started.runUuid))
			return nil
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

// runStart holds either the run meshStack started or the reason it could not, along with the
// building block as meshStack reported it then.
type runStart struct {
	raw     jsontext.Value
	runUuid string
	failure string
}

// awaitRunStart polls the building block until it reports something the trigger-run answer did not:
// a new latest run, or a run start failure recorded anew.
func awaitRunStart(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID, accepted *client.MeshBuildingBlockV2Status, every, within time.Duration) (runStart, error) {
	if accepted == nil {
		accepted = &client.MeshBuildingBlockV2Status{}
	}
	ctx, cancel := context.WithTimeoutCause(ctx, within, fmt.Errorf(
		"meshStack accepted the run of building block %s, but had not started it %s later. `meshstack buildingblockrun list --building-block %s --limit 1` shows the run once it has",
		buildingBlockUuid, within, buildingBlockUuid))
	defer cancel()
	for {
		current, err := raw.Get[client.MeshBuildingBlockV2](ctx, buildingBlockUuid)
		if err != nil && ctx.Err() != nil {
			return runStart{}, context.Cause(ctx)
		} else if err != nil {
			return runStart{}, err
		}
		var block struct {
			Status *client.MeshBuildingBlockV2Status `json:"status"`
		}
		if err := json.Unmarshal(current, &block); err != nil {
			return runStart{}, err
		}
		if status := block.Status; status != nil {
			switch {
			case status.LatestRunUuid != nil && *status.LatestRunUuid != deref(accepted.LatestRunUuid):
				return runStart{raw: current, runUuid: *status.LatestRunUuid}, nil
			case status.RunStartFailure != nil && deref(status.RunStartFailedOn) != deref(accepted.RunStartFailedOn):
				return runStart{raw: current, failure: *status.RunStartFailure}, nil
			}
		}
		select {
		case <-ctx.Done():
			return runStart{}, context.Cause(ctx)
		case <-time.After(every):
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
