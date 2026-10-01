package buildingblockrun

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		flags             internal.ListFlags
		buildingBlockUuid string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building block runs, newest first",
		Long: `List building block runs, newest first.

--building-block lists that block's runs. --workspace, or MESHSTACK_WORKSPACE, then only picks
the workspace a login acts in. Without --building-block the runs of every building block the
credential can see are listed, one block after the other, the newest block first, and
--workspace, or MESHSTACK_WORKSPACE, narrows those to the building blocks of that workspace.`,
		Example: `  meshstack buildingblockrun list --building-block 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bbrun list --workspace my-workspace --limit 20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if buildingBlockUuid != "" {
				// The backend answers a uuid it does not know, or no uuid at all, with an empty list.
				if _, err := uuid.Parse(buildingBlockUuid); err != nil {
					return fmt.Errorf("--building-block %q is no uuid", buildingBlockUuid)
				}
				return flags.Run[client.MeshBuildingBlockRun](cmd, client.MeshBuildingBlockRunListFilter{BuildingBlockUuid: buildingBlockUuid})
			}
			var (
				blockFilter client.MeshBuildingBlockV2ListFilter
				err         error
			)
			if blockFilter.WorkspaceIdentifier, err = internal.ListWorkspace(cmd.Context()); err != nil {
				return err
			}
			return flags.RunSeq(cmd, func(ctx context.Context, meshStack client.Client, options client.ListOptions) iter.Seq2[jsontext.Value, error] {
				return allRuns(ctx, meshStack, blockFilter, options.PageSize)
			})
		},
	}

	cmd.Flags().StringVar(&buildingBlockUuid, "building-block", "", "list the runs of the building block with this uuid")
	flags.Register(cmd.Flags())

	return cmd
}

// allRuns flattens the runs of every building block into one sequence, because the run list
// endpoint takes one building block at a time and has no list of every run.
func allRuns(ctx context.Context, meshStack client.Client, blockFilter client.MeshBuildingBlockV2ListFilter, pageSize int) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		// The blocks are read raw because only their uuid is needed, and a block the client cannot
		// fully decode still has runs to list.
		for rawBlock, err := range meshStack.Raw.List[client.MeshBuildingBlockV2](ctx, blockFilter, client.ListOptions{}) {
			if err != nil {
				yield(nil, err)
				return
			}
			var buildingBlock struct {
				Metadata struct {
					Uuid string `json:"uuid"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(rawBlock, &buildingBlock); err != nil {
				yield(nil, err)
				return
			}
			if buildingBlock.Metadata.Uuid == "" {
				yield(nil, errors.New("building block has no metadata.uuid: this must be a bug in the meshStack CLI or meshStack"))
				return
			}
			filter := client.MeshBuildingBlockRunListFilter{BuildingBlockUuid: buildingBlock.Metadata.Uuid}
			for blockRun, runErr := range meshStack.Raw.List[client.MeshBuildingBlockRun](ctx, filter, client.ListOptions{PageSize: pageSize}) {
				if runErr != nil {
					yield(nil, fmt.Errorf("listing the runs of building block %s: %w", filter.BuildingBlockUuid, runErr))
					return
				}
				if !yield(blockRun, nil) {
					return
				}
			}
		}
	}
}
