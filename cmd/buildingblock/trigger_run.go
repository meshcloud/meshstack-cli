package buildingblock

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types/hal"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/buildingblock"
)

func newTriggerRun() *cobra.Command {
	var (
		output internal.OutputFlag
		wait   bool
	)

	cmd := &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block. Give the building block by its uuid; 'meshstack buildingblock list' shows it as metadata.uuid.

The command waits until meshStack has started the run, and then prints the building block. Its status.latestRunUuid is the run. If the definition requires an approval of the run, status.latestDryRunUuid is the preflight run, whose plan the approver sees. 'meshstack buildingblockrun logs <run-uuid>' shows how far a run got.

With --` + waitFlagName + `, the command waits until the run has ended. This is the same as piping meshStack's response to the run request into 'meshstack buildingblock wait <uuid> --` + beforeFlagName + `=-'.

'meshstack api-docs --describe buildingblock.trigger-run' shows the fields of the response.`,
		Example: `  meshstack buildingblock trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bb trigger-run 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 -o ndjson | jq -r .status.latestRunUuid`,
		Args: internal.UuidArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			buildingBlockUuid := args[0]
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			block, err := meshStack.BuildingBlockV2.Read(ctx, buildingBlockUuid)
			switch {
			case err != nil:
				return err
			case block == nil:
				return fmt.Errorf("building block %s not found", buildingBlockUuid)
			}
			answer, err := block.Links.Post[client.MeshBuildingBlockV2](ctx, "triggerRun", client.MeshBuildingBlockV2RunTrigger{})
			if errors.Is(err, hal.ErrNoLink) {
				return fmt.Errorf("meshStack lets you trigger no run of building block %s", buildingBlockUuid)
			}
			if err != nil {
				return err
			}
			var accepted client.MeshBuildingBlockV2Status
			if answer.Status != nil {
				accepted = *answer.Status
			}
			if wait {
				return awaitRunEnd(ctx, cmd.OutOrStdout(), output.Format, meshStack.BuildingBlockV2, buildingBlockUuid, accepted)
			}
			runWait := buildingblock.NewRunWait(meshStack.BuildingBlockV2, buildingBlockUuid)
			reacted, err := runWait.AwaitReaction(ctx, accepted)
			if err != nil {
				return err
			}
			if err := output.Format.WriteItem(cmd.OutOrStdout(), client.WithKindAndApiVersion(meshStack.BuildingBlockV2, *reacted)); err != nil {
				return err
			}
			if err := runWait.StartFailure(reacted); err != nil {
				return err
			}
			slog.InfoContext(ctx, startedRunMessage(buildingBlockUuid, accepted, *reacted.Status))
			return nil
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().BoolVar(&wait, waitFlagName, false, "wait until the run has ended, and fail if the run failed or is waiting for a person or a dependency")

	return cmd
}

const waitFlagName = "wait"

func startedRunMessage(buildingBlockUuid string, accepted, reacted client.MeshBuildingBlockV2Status) string {
	switch dryRun, run := reacted.GetLatestDryRunUuid(), reacted.GetLatestRunUuid(); {
	case dryRun != "" && dryRun != accepted.GetLatestDryRunUuid():
		return fmt.Sprintf("meshStack started the preflight run %s of building block %s. The run that makes the changes then waits for approval. "+
			"Follow the preflight run with 'meshstack buildingblockrun logs %s'", dryRun, buildingBlockUuid, dryRun)
	case run != "" && run != accepted.GetLatestRunUuid():
		return fmt.Sprintf("meshStack started run %s of building block %s. Follow it with 'meshstack buildingblockrun logs %s'",
			run, buildingBlockUuid, run)
	default:
		return fmt.Sprintf("Building block %s now has the action %s.", buildingBlockUuid, reacted.Action)
	}
}
