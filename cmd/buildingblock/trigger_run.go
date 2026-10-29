package buildingblock

import (
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/buildingblock"
)

func newTriggerRun() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
		wait              bool
	)

	cmd := &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block, named by its uuid, which "meshstack buildingblock list"
reports as metadata.uuid.

The command waits until meshStack has started the run, and writes the building block, whose
status.latestRunUuid names the run. Where the definition asks for an approval of the run,
status.latestDryRunUuid names the preflight run, whose plan the approver sees.
"meshstack buildingblockrun logs <run-uuid>" shows how far a run got.
With --wait, the command waits until the run has ended, the same as piping the answer of
meshStack to the request for the run into "meshstack buildingblock wait <uuid> --before=-".
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
			answer, err := meshStack.BuildingBlockV2.TriggerRun(ctx, buildingBlockUuid.String())
			if err != nil {
				return err
			}
			var accepted client.MeshBuildingBlockV2Status
			if answer.Status != nil {
				accepted = *answer.Status
			}
			if wait {
				return awaitRunEnd(ctx, cmd.OutOrStdout(), output.Format, meshStack.Raw, buildingBlockUuid, accepted)
			}
			runWait := buildingblock.NewRunWait(meshStack.Raw, buildingBlockUuid)
			reacted, block, err := runWait.AwaitReaction(ctx, accepted)
			if err != nil {
				return err
			}
			if err := output.Format.WriteItem(cmd.OutOrStdout(), reacted); err != nil {
				return err
			}
			if err := runWait.StartFailure(block); err != nil {
				return err
			}
			slog.InfoContext(ctx, startedRunMessage(buildingBlockUuid, accepted, *block.Status))
			return nil
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().BoolVar(&wait, "wait", false, "wait until the run has ended, and fail when it failed or waits for someone")

	return cmd
}

func startedRunMessage(buildingBlockUuid uuid.UUID, accepted, reacted client.MeshBuildingBlockV2Status) string {
	switch dryRun, run := reacted.GetLatestDryRunUuid(), reacted.GetLatestRunUuid(); {
	case dryRun != "" && dryRun != accepted.GetLatestDryRunUuid():
		return fmt.Sprintf("meshStack started the preflight run %s of building block %s. A run that changes something waits for approval "+
			"after it. Follow it with `meshstack buildingblockrun logs %s`", dryRun, buildingBlockUuid, dryRun)
	case run != "" && run != accepted.GetLatestRunUuid():
		return fmt.Sprintf("meshStack started run %s of building block %s. Follow it with `meshstack buildingblockrun logs %s`",
			run, buildingBlockUuid, run)
	default:
		return fmt.Sprintf("Building block %s now has the action %s.", buildingBlockUuid, reacted.Action)
	}
}
