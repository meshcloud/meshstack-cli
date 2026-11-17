package buildingblock

import (
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

func newRejectRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "reject-run <building-block-uuid>",
		Short: "Reject a building block run that is waiting for approval",
		Long: `Reject the latest run of a building block, if the run is waiting for approval.

The run ends at once without applying its plan, and meshStack does not plan the change again. Trigger a new run if you want the change applied later. The command shows the run and asks you to confirm, so it needs a terminal.

A building block whose status.action is APPROVE_RUN in 'meshstack buildingblock list' has a run that is waiting for approval. After the rejection, the command prints the building block.`,
		Example: `  meshstack buildingblock reject-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb reject-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			action, err := newRunAction(cmd, buildingBlockUuid, output.Format)
			if err != nil {
				return err
			}
			return action.reject(ctx)
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

func (a runAction) reject(ctx context.Context) error {
	_, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	if block.Links.LatestRun == nil {
		return fmt.Errorf("building block %s has no run to reject", a.buildingBlockUuid)
	}
	run, err := a.meshStack.BuildingBlockRunAction.ReadRun(ctx, *block.Links.LatestRun)
	if err != nil {
		return err
	}
	if run.Links.Reject == nil {
		return fmt.Errorf("the latest run %s of building block %s (action %s) is not waiting for approval, or you are not allowed to reject it",
			run.Metadata.Uuid, a.buildingBlockUuid, block.Status.Action)
	}
	if err = a.prompt.Printf("Run %d of building block %q is waiting for approval. Rejecting it ends the run without applying its plan.\n",
		run.Spec.RunNumber, run.Spec.BuildingBlock.Spec.DisplayName); err != nil {
		return err
	}
	confirmed, err := a.prompt.Confirm(ctx, "Reject this run?")
	if err != nil {
		return err
	}
	if !confirmed {
		slog.InfoContext(ctx, fmt.Sprintf("Did not reject run %s.", run.Metadata.Uuid))
		return nil
	}
	err = a.meshStack.BuildingBlockRunAction.RejectRun(ctx, *run.Links.Reject)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok && httpErr.StatusCode == gohttp.StatusConflict {
		return fmt.Errorf("run %s is no longer waiting for approval", run.Metadata.Uuid)
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Rejected run %s.", run.Metadata.Uuid))
}
