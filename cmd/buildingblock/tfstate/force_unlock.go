package tfstate

import (
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
)

func newForceUnlock() *cobra.Command {
	var (
		buildingBlockUuid uuid.UUID
		force             bool
	)

	cmd := &cobra.Command{
		Use:   "force-unlock <building-block-uuid>",
		Short: "Release a stale lock on the state of a building block",
		Long: `Release a lock on the state of a building block that its holder left behind, for example
because a tofu process was killed. Give the building block by its uuid. Unlike tofu force-unlock,
this command needs no module directory and no lock ID. It shows who holds the lock, and asks before
it releases the lock. Only the answer yes releases the lock; a script can pipe yes to stdin.

If the lock belongs to a run of the building block that is pending or in progress, the command
refuses to release it, because that run is still writing the state. Use --force to release it
anyway. --force does not skip the question.

By default, the command uses the state stored in the building block's own workspace,
metadata.ownedByWorkspace. Use --` + internal.WorkspaceFlag.Name.String() + ` to choose another workspace.`,
		Example: `  meshstack bb tfstate force-unlock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  echo yes | meshstack bb tfstate force-unlock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --force`,
		Args: internal.UuidArg(&buildingBlockUuid),
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
			lock, err := store.ReadLock(ctx)
			if errors.Is(err, tfstate.ErrNoLock) {
				slog.InfoContext(ctx, "The state of building block "+buildingBlockUuid.String()+" in workspace "+store.Workspace+" has no lock")
				return nil
			} else if err != nil {
				return withRightsHint(err)
			}
			slog.InfoContext(ctx, "The state of building block "+buildingBlockUuid.String()+" is locked by "+lock.String())

			if !force {
				if running := lock.NoUnfinishedRun(ctx, meshStack.Raw); running != nil {
					return fmt.Errorf("%w: wait for it to finish, or run again with --force", running)
				}
			}
			p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err = p.Printf("Release the lock %s? Type yes to release it: ", lock.Info.ID); err != nil {
				return err
			}
			answer, err := p.Next(ctx, "confirmation")
			if err != nil {
				return fmt.Errorf("kept the lock %s: %w", lock.Info.ID, err)
			} else if answer != "yes" {
				return fmt.Errorf("kept the lock %s: the answer was %q, and only yes releases the lock", lock.Info.ID, answer)
			}
			if err = store.Release(ctx, lock); errors.Is(err, tfstate.ErrLockReplaced) {
				return fmt.Errorf("%w; run again to see the new one", err)
			} else if err != nil {
				return withRightsHint(err)
			}
			slog.InfoContext(ctx, "Released the lock "+lock.Info.ID+" on the state of building block "+buildingBlockUuid.String())
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "release the lock even if it belongs to a run that is pending or in progress")

	return cmd
}
