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
		runUuid           uuid.UUID
		detectRun         uuid.UUID
	)

	cmd := &cobra.Command{
		Use:   "approve-run <building-block-uuid>",
		Short: "Approve the plan of a building block run that is waiting for approval",
		Long: `Approve the plan of the latest run of a building block, if the run is waiting for approval.

The command shows the plan and asks you to approve it, so it needs a terminal. It approves exactly the plan it showed. If meshStack planned the run again in the meantime, the approval fails, and you can run the command again to review the new plan.

Without a terminal, review the plan with 'meshstack buildingblockrun plan <run-uuid>' first, and approve it only if the user allows it. Then pass the uuid of that run with --` + runFlagName + ` and the uuid of the detect run that 'meshstack buildingblockrun plan' prints with --` + detectRunFlagName + `. The command approves that plan of that run without asking. If meshStack has planned the run again since, the approval fails, and the user has to see the new plan before you approve it.

A building block whose status.action is APPROVE_RUN in 'meshstack buildingblock list' has a run that is waiting for approval. After the approval, the command prints the building block.`,
		Example: `  meshstack buildingblock approve-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb approve-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --run 7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c --detect-run 2c8d1e4f-6a7b-4c3d-8e9f-0a1b2c3d4e5f`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			named := cmd.Flags().Changed(runFlagName)
			action, err := newRunAction(cmd, buildingBlockUuid, output.Format, named)
			if err != nil {
				return err
			}
			if named {
				return action.approveNamedRun(ctx, runUuid, detectRun)
			}
			return action.approve(ctx)
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().Var((*internal.UuidFlag)(&runUuid), runFlagName, "approve the run with this uuid without asking, together with --"+detectRunFlagName)
	cmd.Flags().Var((*internal.UuidFlag)(&detectRun), detectRunFlagName, "approve the plan of the detect run with this uuid, together with --"+runFlagName)
	cmd.MarkFlagsRequiredTogether(runFlagName, detectRunFlagName)

	return cmd
}

const detectRunFlagName = "detect-run"

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
	plan, err := a.confirmPlan(ctx, run)
	if err != nil || plan == "" {
		return err
	}
	return a.sendApproval(ctx, run, plan,
		fmt.Errorf("the plan of run %s changed after you saw it. Run the command again to review the new plan", run.Metadata.Uuid))
}

func (a runAction) approveNamedRun(ctx context.Context, runUuid, detectRun uuid.UUID) error {
	run, err := a.readNamedRun(ctx, runUuid)
	if err != nil {
		return err
	}
	if run.Links.Approve == nil {
		return fmt.Errorf("run %s of building block %s is not waiting for approval, or you are not allowed to approve it. 'meshstack buildingblock list' shows the run that waits as status.latestRunUuid of a building block whose status.action is APPROVE_RUN",
			runUuid, a.buildingBlockUuid)
	}
	return a.sendApproval(ctx, run, detectRun.String(), plansDiffer(run, detectRun))
}

// sendApproval approves the plan of the detect run plan for run, and prints the building block.
// changed is the error for the 409 of meshStack, which it sends for any plan that is not the one
// the run waits for now.
func (a runAction) sendApproval(ctx context.Context, run client.MeshBuildingBlockRun, plan string, changed error) error {
	err := a.meshStack.BuildingBlockRunAction.ApproveRun(ctx, *run.Links.Approve, plan)
	if httpErr, ok := errors.AsType[client.HttpError](err); ok {
		switch httpErr.StatusCode {
		case gohttp.StatusConflict:
			return changed
		case gohttp.StatusBadRequest:
			return fmt.Errorf("run %s is no longer waiting for approval", run.Metadata.Uuid)
		}
	}
	if err != nil {
		return err
	}
	return a.report(ctx, fmt.Sprintf("Approved the plan of run %s. Follow the run with 'meshstack buildingblockrun logs %s --follow'.", run.Metadata.Uuid, run.Metadata.Uuid))
}

// plansDiffer is the error for the 409 of a caller that named the detect run: meshStack planned the
// run again, or the caller named a detect run that is not the one of the run.
func plansDiffer(run client.MeshBuildingBlockRun, detectRun uuid.UUID) error {
	return fmt.Errorf("run %s waits for another plan than the one of detect run %s: meshStack planned the run again, or that is not its detect run. "+
		"An earlier go-ahead does not count for the new plan. Show the user the plan from 'meshstack buildingblockrun plan %s', and approve it only if the user allows it",
		run.Metadata.Uuid, detectRun, run.Metadata.Uuid)
}

// confirmPlan shows the plan that run waits to have approved, and returns its detect run if the
// user approves it.
func (a runAction) confirmPlan(ctx context.Context, run client.MeshBuildingBlockRun) (string, error) {
	plan, logs, err := buildingblockrun.ReadPlan(ctx, a.meshStack, run)
	if err != nil {
		return "", err
	}
	var shown bytes.Buffer
	if err = buildingblockrun.WriteLogs(&shown, logs); err != nil {
		return "", err
	}
	if err = a.prompt.Printf("Run %d of building block %q is waiting for approval of the plan from run %s:\n\n%s\n",
		run.Spec.RunNumber, run.Spec.BuildingBlock.Spec.DisplayName, plan.Metadata.Uuid, shown.String()); err != nil {
		return "", err
	}
	approved, err := a.prompt.Confirm(ctx, "Approve this plan?")
	if err != nil {
		return "", err
	}
	if !approved {
		slog.InfoContext(ctx, fmt.Sprintf("Did not approve run %s.", run.Metadata.Uuid))
		return "", nil
	}
	return plan.Metadata.Uuid, nil
}
