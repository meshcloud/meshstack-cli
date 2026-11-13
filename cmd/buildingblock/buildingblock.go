package buildingblock

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/tfstate"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
)

func New() *cobra.Command {
	const command internal.KindCommand = "buildingblock"
	const short = "Work with meshStack building blocks"
	cmd := &cobra.Command{
		Use:     string(command),
		Aliases: command.Aliases(),
		Short:   short,
		Long:    command.Long(short),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newList())
	cmd.AddCommand(newTriggerRun())
	cmd.AddCommand(newWait())
	cmd.AddCommand(newApproveRun())
	cmd.AddCommand(newAbortRun())
	cmd.AddCommand(tfstate.New())

	return cmd
}

type runAction struct {
	meshStack         client.Client
	prompt            prompt.Prompt
	buildingBlockUuid string
	out               io.Writer
	format            internal.OutputFormat
}

func newRunAction(cmd *cobra.Command, buildingBlockUuid string, format internal.OutputFormat) (runAction, error) {
	p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
	// A script cannot approve or abort: the decision belongs to a person who saw the run.
	if !p.UsesTerminal() {
		return runAction{}, fmt.Errorf("%s needs a terminal, because it asks you to confirm", cmd.Name())
	}
	meshStack, err := internal.ResolveClient(cmd.Context())
	if err != nil {
		return runAction{}, err
	}
	return runAction{meshStack: meshStack, prompt: p, buildingBlockUuid: buildingBlockUuid, out: cmd.OutOrStdout(), format: format}, nil
}

func (a runAction) readBuildingBlock(ctx context.Context) (client.MeshBuildingBlockV2, error) {
	block, err := a.meshStack.BuildingBlockV2.Read(ctx, a.buildingBlockUuid)
	switch {
	case err != nil:
		return client.MeshBuildingBlockV2{}, err
	case block == nil:
		return client.MeshBuildingBlockV2{}, fmt.Errorf("building block %s not found", a.buildingBlockUuid)
	}
	return *block, nil
}

func (a runAction) readRun(ctx context.Context, runUuid string) (client.MeshBuildingBlockRun, error) {
	run, err := a.meshStack.BuildingBlockRun.Read(ctx, runUuid)
	switch {
	case err != nil:
		return client.MeshBuildingBlockRun{}, err
	case run == nil:
		return client.MeshBuildingBlockRun{}, fmt.Errorf("run %s of building block %s not found", runUuid, a.buildingBlockUuid)
	}
	return *run, nil
}

// report reads the building block again and writes it, because a run hides the actions
// APPROVE_RUN and REVIEW_ABORTED_RUN that only the building block shows.
func (a runAction) report(ctx context.Context, done string) error {
	block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	if err := a.format.WriteItem(a.out, client.WithKindAndApiVersion(a.meshStack.BuildingBlockV2, block)); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("%s Building block %s now has the action %s.", done, a.buildingBlockUuid, block.Status.Action))
	return nil
}
