package buildingblock

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/buildingblock"
)

func newWait() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
		beforeInput       internal.JsonInputFlag[client.MeshBuildingBlockV2]
	)

	cmd := &cobra.Command{
		Use:   "wait <building-block-uuid> [--before <file|->]",
		Short: "Wait until the run of a building block has ended",
		Long: `Wait until the run of a building block has ended. Give the building block by its uuid.

Without --` + beforeFlagName + `, the command waits for the run that is going on now, such as a run that someone started or approved. If no run is going on, the command prints the building block at once.

After a write of the building block, pass meshStack's response to the write with --` + beforeFlagName + `. meshStack responds to a write before it starts the run, so right after the write, the building block still shows the action of the previous run. The response can be that of a create (POST), an update (PUT) or a trigger-run, or a read taken before the change. The command first waits up to a minute until meshStack has reacted, following the rules of https://docs.meshcloud.io/concepts/building-block/#waiting-for-a-run, and then until the run has ended.

Press Ctrl-C to stop waiting. The command prints the building block. It fails and names the action if the run failed, was aborted or could not start, or if the run is waiting for a person or a dependency.`,
		Example: `  meshstack bb wait 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack api -X PUT /api/meshobjects/meshbuildingblocks/0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --request-json block.json |
    meshstack bb wait 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --before=-`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var before *client.MeshBuildingBlockV2Status
			if cmd.Flags().Changed(beforeFlagName) {
				read, err := readBefore(cmd, beforeInput, buildingBlockUuid)
				if err != nil {
					return err
				}
				before = &read
			}
			meshStack, err := internal.ResolveClient(cmd.Context())
			if err != nil {
				return err
			}
			return awaitRunEnd(cmd.Context(), cmd.OutOrStdout(), output.Format, meshStack.Raw, buildingBlockUuid, before)
		},
	}

	output.RegisterForItem(cmd.Flags())
	beforeInput.Register(cmd.Flags(), beforeFlagName, "file with the building block from before the write, or - to read it from stdin")

	return cmd
}

const beforeFlagName = "before"

func readBefore(cmd *cobra.Command, input internal.JsonInputFlag[client.MeshBuildingBlockV2], buildingBlockUuid uuid.UUID) (client.MeshBuildingBlockV2Status, error) {
	source := input.Source
	before, err := input.Read(cmd)
	if err != nil {
		return client.MeshBuildingBlockV2Status{}, err
	}
	switch {
	case before.Metadata.Uuid != nil && *before.Metadata.Uuid != buildingBlockUuid.String():
		return client.MeshBuildingBlockV2Status{}, fmt.Errorf("--%s %s contains building block %s, but the command waits for %s", beforeFlagName, source, *before.Metadata.Uuid, buildingBlockUuid)
	case before.Status == nil:
		return client.MeshBuildingBlockV2Status{}, fmt.Errorf("--%s %s contains no status of the building block", beforeFlagName, source)
	}
	return *before.Status, nil
}

// awaitRunEnd writes the building block once its run has ended, and fails on an outcome someone has
// to act on. A nil before waits for the run going on now.
func awaitRunEnd(ctx context.Context, out io.Writer, format internal.OutputFormat, raw *client.RawClient, buildingBlockUuid uuid.UUID, before *client.MeshBuildingBlockV2Status) error {
	wait := buildingblock.NewRunWait(raw, buildingBlockUuid)
	ended, block, err := wait.AwaitEnd(ctx, before)
	if err != nil {
		return err
	}
	if err = format.WriteItem(out, ended); err != nil {
		return err
	}
	if err = wait.Outcome(ctx, block); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("The run of building block %s ended with action %s.", buildingBlockUuid, block.Status.Action))
	return nil
}
