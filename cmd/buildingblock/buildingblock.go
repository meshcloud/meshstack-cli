package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/tfstate"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockrun"
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
	buildingBlockUuid uuid.UUID
	out               io.Writer
	format            internal.OutputFormat
}

// newRunAction needs a terminal to ask for a confirmation, unless the caller names the plan to approve
// or the run to abort. A name binds the command to that plan or run, but does not show that anyone
// has reviewed it.
func newRunAction(cmd *cobra.Command, buildingBlockUuid uuid.UUID, format internal.OutputFormat, confirmed bool) (runAction, error) {
	p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
	if !confirmed && !p.UsesTerminal() {
		return runAction{}, fmt.Errorf("%s needs a terminal, because it asks you to confirm. '%s --help' shows how to run it without a terminal", cmd.Name(), cmd.CommandPath())
	}
	meshStack, err := internal.ResolveClient(cmd.Context())
	if err != nil {
		return runAction{}, err
	}
	return runAction{meshStack: meshStack, prompt: p, buildingBlockUuid: buildingBlockUuid, out: cmd.OutOrStdout(), format: format}, nil
}

type linkedBuildingBlock struct {
	Status client.MeshBuildingBlockV2Status `json:"status"`
	Links  client.MeshBuildingBlockV2Links  `json:"_links"`
}

func (a runAction) readBuildingBlock(ctx context.Context) (jsontext.Value, linkedBuildingBlock, error) {
	var block linkedBuildingBlock
	raw, err := a.meshStack.Raw.Get[client.MeshBuildingBlockV2](ctx, a.buildingBlockUuid)
	if err != nil {
		return nil, block, err
	}
	return raw, block, json.Unmarshal(raw, &block)
}

const runFlagName = "run"

// readNamedRun reads the run that the caller names with --run, which has to belong to the building block.
func (a runAction) readNamedRun(ctx context.Context, runUuid uuid.UUID) (client.MeshBuildingBlockRun, error) {
	run, err := buildingblockrun.ReadRun(ctx, a.meshStack, runUuid)
	if err != nil {
		return run, err
	}
	if run.Spec.BuildingBlock.Uuid != a.buildingBlockUuid.String() {
		return run, fmt.Errorf("run %s belongs to building block %s, not to %s", runUuid, run.Spec.BuildingBlock.Uuid, a.buildingBlockUuid)
	}
	return run, nil
}

// report reads the building block again and writes it, because a run hides the actions
// APPROVE_RUN and REVIEW_ABORTED_RUN that only the building block shows.
func (a runAction) report(ctx context.Context, done string) error {
	raw, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	if err := a.format.WriteItem(a.out, raw); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("%s Building block %s now has the action %s.", done, a.buildingBlockUuid, block.Status.Action))
	return nil
}
