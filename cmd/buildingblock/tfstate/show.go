package tfstate

import (
	"errors"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
)

func newShow() *cobra.Command {
	var buildingBlockUuid uuid.UUID

	return &cobra.Command{
		Use:   "show <building-block-uuid>",
		Short: "Show the state of a building block",
		Long: `Show the state of a building block, named by its uuid, as meshStack stores it. For a building
block that has no state yet, the command writes nothing and succeeds.

The state is the one stored under the workspace of the building block, metadata.ownedByWorkspace,
unless --workspace names another one.`,
		Example: `  meshstack bb tfstate show 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 | jq .resources`,
		Args:    internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			store, err := openStore(ctx, meshStack, buildingBlockUuid)
			if err != nil {
				return err
			}
			state, err := store.Get(ctx)
			if errors.Is(err, tfstate.ErrNoState) {
				slog.InfoContext(ctx, "Building block "+buildingBlockUuid.String()+" has no state in workspace "+store.Workspace+" yet")
				return nil
			} else if err != nil {
				return withApiKeyHint(err)
			}
			_, err = cmd.OutOrStdout().Write(state)
			return err
		},
	}
}
