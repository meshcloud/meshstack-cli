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
		Short: "Approve the plan of a building block run that waits for approval",
		Long: `Approve the plan of the latest run of a building block, when the run waits for approval.

The command shows the plan and asks you to approve it, so it runs only on a terminal. It approves
exactly the plan it showed. If meshStack planned the run again in the meantime, the approval fails,
and you can run the command again to review the new plan.

"meshstack buildingblock list --status WAITING_FOR_APPROVAL" lists the building blocks with a run
that waits for approval. After the approval, the command writes the building block.`,
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
		return fmt.Errorf("building block %s has the status %s. Its latest run %s does not wait for approval, or you may not approve it",
			a.buildingBlockUuid, block.Status.Status, run.Metadata.Uuid)
	}
	if run.Links.Predecessor == nil {
		return fmt.Errorf("run %s waits for approval, but meshStack names no run that planned it", run.Metadata.Uuid)
	}
	plan, err := a.meshStack.BuildingBlockRunAction.ReadRun(ctx, *run.Links.Predecessor)
	if err != nil {
		return err
	}
	if plan.Links.DownloadLogs == nil {
		return fmt.Errorf("meshStack shows you no logs of run %s, which planned run %s", plan.Metadata.Uuid, run.Metadata.Uuid)
	}
	logs, err := a.meshStack.BuildingBlockRunAction.ReadLogs(ctx, *plan.Links.DownloadLogs)
	if err != nil {
		return err
	}
	var shown bytes.Buffer
	if err = buildingblockrun.WriteLogs(&shown, logs); err != nil {
		return err
	}
	if err = a.prompt.Printf("Run %d of building block %q waits for approval of the plan of run %s:\n\n%s\n",
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
			return fmt.Errorf("run %s no longer waits for approval", run.Metadata.Uuid)
		}
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Approved the plan of run %s.", run.Metadata.Uuid))
}
