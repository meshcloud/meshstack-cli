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
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newAbortRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "abort-run <building-block-uuid>",
		Short: "Abort the latest run of a building block",
		Long: `Abort the latest run of a building block, or its dry run when the dry run is newer.

The command shows the run and asks you to confirm, so it runs only on a terminal. After the abort,
it writes the building block.`,
		Example: `  meshstack buildingblock abort-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb abort-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			action, err := newRunAction(cmd, buildingBlockUuid, output.Format)
			if err != nil {
				return err
			}
			return action.abort(ctx)
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

func (a runAction) abort(ctx context.Context) error {
	_, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	latest := cmp.Or(block.Links.LatestDryRun, block.Links.LatestRun)
	if latest == nil {
		return fmt.Errorf("building block %s has no run to abort", a.buildingBlockUuid)
	}
	run, err := a.meshStack.BuildingBlockRunAction.ReadRun(ctx, *latest)
	if err != nil {
		return err
	}
	if run.Links.Abort == nil {
		return fmt.Errorf("building block %s has the status %s. Its run %s has the status %s, and it cannot be aborted, or you may not abort it",
			a.buildingBlockUuid, block.Status.Status, run.Metadata.Uuid, run.Status)
	}
	if err = a.prompt.Printf("Run %d of building block %q is a %s run with the status %s. The building block has the status %s.\n",
		run.Spec.RunNumber, run.Spec.BuildingBlock.Spec.DisplayName, run.Spec.Behavior, run.Status, block.Status.Status); err != nil {
		return err
	}
	confirmed, err := a.prompt.Confirm(ctx, "Abort this run?")
	if err != nil {
		return err
	}
	if !confirmed {
		slog.InfoContext(ctx, fmt.Sprintf("Did not abort run %s.", run.Metadata.Uuid))
		return nil
	}
	err = a.meshStack.BuildingBlockRunAction.AbortRun(ctx, *run.Links.Abort)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok && httpErr.StatusCode == gohttp.StatusBadRequest {
		return fmt.Errorf("run %s can no longer be aborted", run.Metadata.Uuid)
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Asked meshStack to abort run %s.", run.Metadata.Uuid))
}
