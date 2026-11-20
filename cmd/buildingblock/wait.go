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
	)

	cmd := &cobra.Command{
		Use:   "wait <building-block-uuid>",
		Short: "Wait until the run of a building block has ended",
		Long: `Wait until the run of a building block has ended. Give the building block by its uuid.

After a write that asks for a run, such as a create (POST), an update (PUT) or a trigger-run, meshStack shows the action PENDING until it has started the run, and IN_PROGRESS while the run is going on. The command waits while the action is PENDING or IN_PROGRESS. Press Ctrl-C to stop waiting.

The command prints the building block. It fails and names the action if the run failed, was aborted or could not start, or if the run is waiting for a person or a dependency.`,
		Example: `  meshstack api -X PUT /api/meshobjects/meshbuildingblocks/0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --request-json block.json &&
    meshstack bb wait 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			meshStack, err := internal.ResolveClient(cmd.Context())
			if err != nil {
				return err
			}
			return awaitRunEnd(cmd.Context(), cmd.OutOrStdout(), output.Format, meshStack.Raw, buildingBlockUuid)
		},
	}

	output.RegisterForItem(cmd.Flags())

	return cmd
}

// awaitRunEnd writes the building block once its run has ended, and fails on an outcome someone has
// to act on.
func awaitRunEnd(ctx context.Context, out io.Writer, format internal.OutputFormat, raw *client.RawClient, buildingBlockUuid uuid.UUID) error {
	wait := buildingblock.NewRunWait(raw, buildingBlockUuid)
	ended, block, err := wait.AwaitEnd(ctx)
	if err != nil {
		return err
	}
	if err = format.WriteItem(out, ended); err != nil {
		return err
	}
	if err = wait.Outcome(block); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("The run of building block %s ended with action %s.", buildingBlockUuid, block.Status.Action))
	return nil
}
