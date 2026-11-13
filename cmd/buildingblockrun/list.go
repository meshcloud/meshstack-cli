package buildingblockrun

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const buildingBlockFlagName = "building-block"

func newList() *cobra.Command {
	var (
		flags             internal.ListFlags
		buildingBlockUuid string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List building block runs, newest first",
		Long: `List building block runs, newest first.

With --` + buildingBlockFlagName + `, the command lists only the runs of that building block. Without it, the command lists the runs of every building block in the workspace. If no workspace is set, it lists the runs of every building block that your credential can access.`,
		Example: `  meshstack buildingblockrun list --building-block 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90
  meshstack bbrun list --workspace my-workspace --limit 20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if buildingBlockUuid != "" {
				meshStack, err := internal.ResolveClient(cmd.Context())
				if err != nil {
					return err
				}
				return flags.Run(cmd, meshStack.BuildingBlockRun, client.MeshBuildingBlockRunFilter{BuildingBlockUuid: buildingBlockUuid})
			}
			var (
				blockFilter client.MeshBuildingBlockV2Filter
				err         error
			)
			if blockFilter.WorkspaceIdentifier, err = internal.ListWorkspace(cmd.Context()); err != nil {
				return err
			}
			meshStack, err := internal.ResolveClient(cmd.Context())
			if err != nil {
				return err
			}
			return flags.RunSeq(cmd, func(ctx context.Context, options ...client.ListOption) iter.Seq2[any, error] {
				return internal.WithKindAndApiVersion(meshStack.BuildingBlockRun, allRuns(ctx, meshStack, blockFilter, options))
			})
		},
	}

	cmd.Flags().Var((*internal.UuidFlag)(&buildingBlockUuid), buildingBlockFlagName, "list the runs of the building block with this uuid")
	flags.Register(cmd.Flags())

	return cmd
}

// runsPageSizeOfEachBlock is small, because a listing reads the first page of every block's runs.
const runsPageSizeOfEachBlock = 10

// allRuns merges the runs of every building block, because the run list endpoint takes one building
// block at a time and has no list of every run.
func allRuns(ctx context.Context, meshStack client.Client, blockFilter client.MeshBuildingBlockV2Filter, options []client.ListOption) iter.Seq2[client.MeshBuildingBlockRun, error] {
	options = append(options, client.WithPageSize(runsPageSizeOfEachBlock))
	return func(yield func(client.MeshBuildingBlockRun, error) bool) {
		var runsOfEachBlock []iter.Seq2[client.MeshBuildingBlockRun, error]
		for block, err := range meshStack.BuildingBlockV2.List(ctx, blockFilter) {
			if err != nil {
				yield(client.MeshBuildingBlockRun{}, err)
				return
			}
			if block.Metadata.Uuid == nil {
				yield(client.MeshBuildingBlockRun{}, errors.New("building block has no metadata.uuid: this must be a bug in the meshStack CLI or meshStack"))
				return
			}
			runsOfEachBlock = append(runsOfEachBlock, runsOf(ctx, meshStack, *block.Metadata.Uuid, options))
		}
		mergeNewestFirst(runsOfEachBlock)(yield)
	}
}

func runsOf(ctx context.Context, meshStack client.Client, buildingBlockUuid string, options []client.ListOption) iter.Seq2[client.MeshBuildingBlockRun, error] {
	return func(yield func(client.MeshBuildingBlockRun, error) bool) {
		filter := client.MeshBuildingBlockRunFilter{BuildingBlockUuid: buildingBlockUuid}
		for run, err := range meshStack.BuildingBlockRun.List(ctx, filter, options...) {
			if err != nil {
				err = fmt.Errorf("listing the runs of building block %s: %w", buildingBlockUuid, err)
			}
			if !yield(run, err) || err != nil {
				return
			}
		}
	}
}

// mergeNewestFirst merges runs that each come newest first. It reads the first run of every one
// before it yields any, and after that reads on only in the runs it yielded the head of. Runs created
// at the same time keep the order of the sequences they come from.
func mergeNewestFirst(runs []iter.Seq2[client.MeshBuildingBlockRun, error]) iter.Seq2[client.MeshBuildingBlockRun, error] {
	return func(yield func(client.MeshBuildingBlockRun, error) bool) {
		heads := make([]*runHead, 0, len(runs))
		stops := make([]func(), 0, len(runs))
		defer func() {
			for _, stop := range stops {
				stop()
			}
		}()
		for _, sequence := range runs {
			next, stop := iter.Pull2(sequence)
			stops = append(stops, stop)
			head := &runHead{next: next}
			if ok, err := head.advance(); err != nil {
				yield(client.MeshBuildingBlockRun{}, err)
				return
			} else if ok {
				heads = append(heads, head)
			}
		}
		for len(heads) > 0 {
			newest := 0
			for i, head := range heads {
				if head.run.Metadata.CreatedAt.After(heads[newest].run.Metadata.CreatedAt) {
					newest = i
				}
			}
			if !yield(heads[newest].run, nil) {
				return
			}
			if ok, err := heads[newest].advance(); err != nil {
				yield(client.MeshBuildingBlockRun{}, err)
				return
			} else if !ok {
				heads = slices.Delete(heads, newest, newest+1)
			}
		}
	}
}

type runHead struct {
	next func() (client.MeshBuildingBlockRun, error, bool)
	run  client.MeshBuildingBlockRun
}

func (h *runHead) advance() (bool, error) {
	run, err, ok := h.next()
	if !ok || err != nil {
		return false, err
	}
	h.run = run
	return true, nil
}
