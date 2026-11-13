package buildingblockrun

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newPlan() *cobra.Command {
	var runUuid uuid.UUID

	return &cobra.Command{
		Use:   "plan <run-uuid>",
		Short: "Show the plan that a building block run is waiting to have approved",
		Long: `Show the plan that a building block run is waiting to have approved, as text.

A run that waits for approval has no logs of its own. Its plan is in the logs of the run before it, the detect run, which planned the changes. The command prints the logs of the detect run and names it. Give the waiting run by its uuid; 'meshstack buildingblock list' shows it as status.latestRunUuid of a building block whose status.action is APPROVE_RUN.

To approve the plan without a terminal, pass the uuid of the detect run to 'meshstack buildingblock approve-run <building-block-uuid> --detect-run <run-uuid>'. The approval fails if meshStack has planned the run again in the meantime.`,
		Example: `  meshstack buildingblockrun plan 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c
  meshstack bbrun plan 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c | grep 'Plan:'`,
		Args: internal.UuidArg(&runUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			run, err := ReadRun(ctx, meshStack, runUuid)
			if err != nil {
				return err
			}
			detectRun, logs, err := ReadPlan(ctx, meshStack, run)
			if err != nil {
				return err
			}
			if err = WriteLogs(cmd.OutOrStdout(), logs); err != nil {
				return err
			}
			if run.Links.Approve == nil {
				slog.InfoContext(ctx, fmt.Sprintf("This is the plan from the detect run %s. Run %s is not waiting for approval, or you are not allowed to approve it.",
					detectRun.Metadata.Uuid, runUuid))
				return nil
			}
			slog.InfoContext(ctx, fmt.Sprintf("Run %s is waiting for approval of this plan from the detect run %s. Approve it with 'meshstack buildingblock approve-run %s --detect-run %s'",
				runUuid, detectRun.Metadata.Uuid, run.Spec.BuildingBlock.Uuid, detectRun.Metadata.Uuid))
			return nil
		},
	}
}

func ReadRun(ctx context.Context, meshStack client.Client, runUuid uuid.UUID) (client.MeshBuildingBlockRun, error) {
	var run client.MeshBuildingBlockRun
	raw, err := meshStack.Raw.Get[client.MeshBuildingBlockRun](ctx, runUuid)
	if err != nil {
		return run, err
	}
	return run, json.Unmarshal(raw, &run)
}

// ReadPlan reads the detect run that planned run, and its logs, which hold the plan that run is
// waiting to have approved.
func ReadPlan(ctx context.Context, meshStack client.Client, run client.MeshBuildingBlockRun) (client.MeshBuildingBlockRun, client.MeshBuildingBlockRunLogs, error) {
	if run.Links.Predecessor == nil {
		return client.MeshBuildingBlockRun{}, client.MeshBuildingBlockRunLogs{}, fmt.Errorf("meshStack does not say which run planned run %s", run.Metadata.Uuid)
	}
	detectRun, err := meshStack.BuildingBlockRunAction.ReadRun(ctx, *run.Links.Predecessor)
	if err != nil {
		return detectRun, client.MeshBuildingBlockRunLogs{}, err
	}
	if detectRun.Links.DownloadLogs == nil {
		return detectRun, client.MeshBuildingBlockRunLogs{}, fmt.Errorf("cannot read the logs of the detect run %s, which planned run %s", detectRun.Metadata.Uuid, run.Metadata.Uuid)
	}
	logs, err := meshStack.BuildingBlockRunAction.ReadLogs(ctx, *detectRun.Links.DownloadLogs)
	return detectRun, logs, err
}
