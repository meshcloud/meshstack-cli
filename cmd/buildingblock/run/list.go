package run

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"iter"

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
		Short: "List building block runs",
		Long: `List building block runs.

--building-block lists that block's runs. Without it the runs of every building block the
credential can see are listed, one block after the other, and --workspace, or
MESHSTACK_WORKSPACE, narrows those to the building blocks of that workspace.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				blockFilter client.MeshBuildingBlockV2ListFilter
				err         error
			)
			if buildingBlockUuid == "" {
				if blockFilter.WorkspaceIdentifier, err = internal.ListWorkspace(cmd.Context()); err != nil {
					return err
				}
			}
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				if buildingBlockUuid != "" {
					return meshStack.Listing.BuildingBlockRuns(ctx, client.MeshBuildingBlockRunListFilter{
						BuildingBlockUuid: buildingBlockUuid,
					})
				}
				return allRuns(ctx, meshStack, blockFilter)
			})
		},
	}

	cmd.Flags().StringVar(&buildingBlockUuid, "building-block", "", "list the runs of the building block with this uuid")
	flags.Register(cmd.Flags())

	return cmd
}

// allRuns flattens the runs of every building block into one sequence, because the run list
// endpoint takes one building block at a time and has no list of every run.
func allRuns(ctx context.Context, meshStack client.Client, blockFilter client.MeshBuildingBlockV2ListFilter) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		runOptions := client.ListOptionsFrom(ctx)
		runOptions.OnPage = nil
		blockOptions := runOptions
		blockOptions.PageSize = 0
		runsCtx := client.WithListOptions(ctx, runOptions)
		// The blocks are read raw because only their uuid is needed, and a block the client cannot
		// fully decode still has runs to list.
		for rawBlock, err := range meshStack.Listing.BuildingBlocksV2(client.WithListOptions(ctx, blockOptions), blockFilter) {
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
				continue
			}
			filter := client.MeshBuildingBlockRunListFilter{BuildingBlockUuid: buildingBlock.Metadata.Uuid}
			for blockRun, runErr := range meshStack.Listing.BuildingBlockRuns(runsCtx, filter) {
				if !yield(blockRun, runErr) || runErr != nil {
					return
				}
			}
		}
	}
}
