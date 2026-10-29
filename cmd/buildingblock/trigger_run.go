package buildingblock

import (
	"errors"
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
	)

	cmd := &cobra.Command{
		Use:   "trigger-run <building-block-uuid>",
		Short: "Ask meshStack to run a building block",
		Long: `Ask meshStack to run a building block, named by its uuid, which "meshstack buildingblock list"
reports as metadata.uuid.

The command waits until meshStack has started the run, and writes the building block, whose
status.latestRunUuid names the run. "meshstack buildingblockrun logs <run-uuid>" shows how far it got.
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
			started, err := buildingblock.AwaitRunStart(ctx, meshStack.Raw, buildingBlockUuid, accepted)
			if err != nil {
				return err
			}
			if err := output.Format.WriteItem(cmd.OutOrStdout(), started.Raw); err != nil {
				return err
			}
			if started.Failure != "" {
				return errors.New(started.Failure)
			}
			slog.InfoContext(ctx, fmt.Sprintf("meshStack started run %s of building block %s. Follow it with `meshstack buildingblockrun logs %s`",
				started.RunUuid, buildingBlockUuid, started.RunUuid))
			return nil
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}
