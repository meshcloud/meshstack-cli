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
	"github.com/meshcloud/meshstack-cli/cmd/buildingblocktfstate"
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
	cmd.AddCommand(newApproveRun())
	cmd.AddCommand(buildingblocktfstate.New())

	return cmd
}

type runAction struct {
	meshStack         client.Client
	prompt            prompt.Prompt
	buildingBlockUuid uuid.UUID
	out               io.Writer
	format            internal.OutputFormat
}

func newRunAction(cmd *cobra.Command, buildingBlockUuid uuid.UUID, format internal.OutputFormat) (runAction, error) {
	p := prompt.New(cmd.InOrStdin(), cmd.ErrOrStderr())
	// A script cannot approve or abort: the decision belongs to a person who saw the run.
	if !p.UsesTerminal() {
		return runAction{}, fmt.Errorf("%s asks you before it acts, so it runs only on a terminal", cmd.Name())
	}
	meshStack, err := internal.ResolveClient(cmd.Context())
	if err != nil {
		return runAction{}, err
	}
	return runAction{meshStack: meshStack, prompt: p, buildingBlockUuid: buildingBlockUuid, out: cmd.OutOrStdout(), format: format}, nil
}

type linkedBuildingBlock struct {
	Status client.MeshBuildingBlockV2Status   `json:"status"`
	Links  client.MeshBuildingBlockV2RunLinks `json:"_links"`
}

func (a runAction) readBuildingBlock(ctx context.Context) (jsontext.Value, linkedBuildingBlock, error) {
	var block linkedBuildingBlock
	raw, err := a.meshStack.Raw.Get[client.MeshBuildingBlockV2](ctx, a.buildingBlockUuid)
	if err != nil {
		return nil, block, err
	}
	return raw, block, json.Unmarshal(raw, &block)
}

// report reads the building block again and writes it, because a run hides the statuses
// WAITING_FOR_APPROVAL and ABORTED that only the building block shows.
func (a runAction) report(ctx context.Context, done string) error {
	raw, block, err := a.readBuildingBlock(ctx)
	if err != nil {
		return err
	}
	if err := a.format.WriteItem(a.out, raw); err != nil {
		return err
	}
	slog.InfoContext(ctx, fmt.Sprintf("%s Building block %s now has the status %s.", done, a.buildingBlockUuid, block.Status.Status))
	return nil
}
