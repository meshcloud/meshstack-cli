package buildingblock

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockrun"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newAbortRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
		runUuid           uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "abort-run <building-block-uuid>",
		Short: "Abort the latest run of a building block",
		Long: `Abort the latest run of a building block, or its dry run if the dry run is newer.

The command shows the run and asks you to confirm, so it needs a terminal. After the abort, it prints the building block.

Without a terminal, pass the uuid of the run to abort with --` + runFlagName + `; 'meshstack buildingblockrun list --building-block <building-block-uuid>' shows it. The command then aborts that run of the building block without asking, also if it is not the latest run.`,
		Example: `  meshstack buildingblock abort-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb abort-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			action, err := newRunAction(cmd, buildingBlockUuid, output.Format, runUuid != uuid.Nil())
			if err != nil {
				return err
			}
			return action.abort(ctx, runUuid)
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().Var((*internal.UuidFlag)(&runUuid), runFlagName, "abort the run with this uuid without asking")

	return cmd
}

const runFlagName = "run"

// abort asks to abort the latest run unless the caller passes the run it decided on.
func (a runAction) abort(ctx context.Context, runUuid uuid.UUID) error {
	_, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	run, err := a.runToAbort(ctx, block, runUuid)
	if err != nil {
		return err
	}
	if run.Links.Abort == nil {
		return fmt.Errorf("run %s of building block %s (action %s) has the status %s and cannot be aborted, or you are not allowed to abort it",
			run.Metadata.Uuid, a.buildingBlockUuid, block.Status.Action, run.Status)
	}
	if runUuid == uuid.Nil() {
		if err = a.prompt.Printf("Run %d of building block %q is a %s run with the status %s. The building block has the action %s.\n",
			run.Spec.RunNumber, run.Spec.BuildingBlock.Spec.DisplayName, run.Spec.Behavior, run.Status, block.Status.Action); err != nil {
			return err
		}
		var confirmed bool
		if confirmed, err = a.prompt.Confirm(ctx, "Abort this run?"); err != nil {
			return err
		}
		if !confirmed {
			slog.InfoContext(ctx, fmt.Sprintf("Did not abort run %s.", run.Metadata.Uuid))
			return nil
		}
	}
	err = a.meshStack.BuildingBlockRunAction.AbortRun(ctx, *run.Links.Abort)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok && httpErr.StatusCode == gohttp.StatusConflict {
		return fmt.Errorf("run %s can no longer be aborted", run.Metadata.Uuid)
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Asked meshStack to abort run %s.", run.Metadata.Uuid))
}

func (a runAction) runToAbort(ctx context.Context, block linkedBuildingBlock, runUuid uuid.UUID) (client.MeshBuildingBlockRun, error) {
	if runUuid != uuid.Nil() {
		run, err := buildingblockrun.ReadRun(ctx, a.meshStack, runUuid)
		if err == nil && run.Spec.BuildingBlock.Uuid != a.buildingBlockUuid.String() {
			err = fmt.Errorf("run %s belongs to building block %s, not to %s", runUuid, run.Spec.BuildingBlock.Uuid, a.buildingBlockUuid)
		}
		return run, err
	}
	latest := cmp.Or(block.Links.LatestDryRun, block.Links.LatestRun)
	if latest == nil {
		return client.MeshBuildingBlockRun{}, fmt.Errorf("building block %s has no run to abort", a.buildingBlockUuid)
	}
	return a.meshStack.BuildingBlockRunAction.ReadRun(ctx, *latest)
}
