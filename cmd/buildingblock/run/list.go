package run

import (
	"context"
	"encoding/json/jsontext"
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
credential can see are listed, one block after the other.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				if buildingBlockUuid != "" {
					return meshStack.BuildingBlockRun.ListRawSeq(ctx, client.MeshBuildingBlockRunListFilter{
						BuildingBlockUuid: buildingBlockUuid,
					})
				}
				return allRuns(ctx, meshStack)
			})
		},
	}

	cmd.Flags().StringVar(&buildingBlockUuid, "building-block", "", "list the runs of the building block with this uuid")
	flags.Register(cmd.Flags())

	return cmd
}

// allRuns flattens the runs of every building block into one sequence, because the run list
// endpoint takes one building block at a time and has no list of every run.
func allRuns(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		runOptions := client.ListOptionsFrom(ctx)
		runOptions.OnPage = nil
		blockOptions := runOptions
		blockOptions.PageSize = 0
		runsCtx := client.WithListOptions(ctx, runOptions)
		for buildingBlock, err := range meshStack.BuildingBlockV2.ListSeq(client.WithListOptions(ctx, blockOptions), client.MeshBuildingBlockV2ListFilter{}) {
			if err != nil {
				yield(nil, err)
				return
			}
			if buildingBlock.Metadata.Uuid == nil {
				continue
			}
			filter := client.MeshBuildingBlockRunListFilter{BuildingBlockUuid: *buildingBlock.Metadata.Uuid}
			for blockRun, runErr := range meshStack.BuildingBlockRun.ListRawSeq(runsCtx, filter) {
				if !yield(blockRun, runErr) || runErr != nil {
					return
				}
			}
		}
	}
}
