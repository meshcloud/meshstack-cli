package buildingblock

import (
	"bytes"
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

func newApproveRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "approve-run <building-block-uuid>",
		Short: "Approve the plan of a building block run that is waiting for approval",
		Long: `Approve the plan of the latest run of a building block, if the run is waiting for approval.

The command shows the plan and asks you to approve it, so it needs a terminal. It approves exactly the plan it showed. If meshStack planned the run again in the meantime, the approval fails, and you can run the command again to review the new plan.

A building block whose status.action is APPROVE_RUN in 'meshstack buildingblock list' has a run that is waiting for approval. After the approval, the command prints the building block.`,
		Example: `  meshstack buildingblock approve-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb approve-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			action, err := newRunAction(cmd, buildingBlockUuid, output.Format)
			if err != nil {
				return err
			}
			return action.approve(ctx)
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

func (a runAction) approve(ctx context.Context) error {
	_, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	if block.Links.LatestRun == nil {
		return fmt.Errorf("building block %s has no run to approve", a.buildingBlockUuid)
	}
	run, err := a.meshStack.BuildingBlockRunAction.ReadRun(ctx, *block.Links.LatestRun)
	if err != nil {
		return err
	}
	if run.Links.Approve == nil {
		return fmt.Errorf("the latest run %s of building block %s (action %s) is not waiting for approval, or you are not allowed to approve it",
			run.Metadata.Uuid, a.buildingBlockUuid, block.Status.Action)
	}
	plan, logs, err := buildingblockrun.ReadPlan(ctx, a.meshStack, run)
	if err != nil {
		return err
	}
	var shown bytes.Buffer
	if err = buildingblockrun.WriteLogs(&shown, logs); err != nil {
		return err
	}
	if err = a.prompt.Printf("Run %d of building block %q is waiting for approval of the plan from run %s:\n\n%s\n",
		run.Spec.RunNumber, run.Spec.BuildingBlock.Spec.DisplayName, plan.Metadata.Uuid, shown.String()); err != nil {
		return err
	}
	approved, err := a.prompt.Confirm(ctx, "Approve this plan?")
	if err != nil {
		return err
	}
	if !approved {
		slog.InfoContext(ctx, fmt.Sprintf("Did not approve run %s.", run.Metadata.Uuid))
		return nil
	}
	err = a.meshStack.BuildingBlockRunAction.ApproveRun(ctx, *run.Links.Approve, plan.Metadata.Uuid)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok {
		switch httpErr.StatusCode {
		case gohttp.StatusConflict:
			return fmt.Errorf("the plan of run %s changed after you saw it. Run the command again to review the new plan", run.Metadata.Uuid)
		case gohttp.StatusBadRequest:
			return fmt.Errorf("run %s is no longer waiting for approval", run.Metadata.Uuid)
		}
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Approved the plan of run %s.", run.Metadata.Uuid))
}
