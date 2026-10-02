package buildingblockrun

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"slices"
	"time"
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
credential can see are listed, and --workspace, or MESHSTACK_WORKSPACE, narrows those to the
building blocks of that workspace. meshStack lists the runs of one building block at a time, so
the command then asks for the first runs of every one of them before it lists the newest.`,
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

// runsPageSizeOfEachBlock caps the page size a limited listing asks for, which is its limit, for the
// runs of each building block: a listing of every building block reads the first page of each one
// before it lists a single run. An unlimited listing reads every page anyway, and takes meshStack's
// default page size.
const runsPageSizeOfEachBlock = 10

// allRuns merges the runs of every building block, because the run list endpoint takes one building
// block at a time and has no list of every run.
func allRuns(ctx context.Context, meshStack client.Client, blockFilter client.MeshBuildingBlockV2ListFilter, pageSize int) iter.Seq2[jsontext.Value, error] {
	if pageSize > 0 {
		pageSize = min(pageSize, runsPageSizeOfEachBlock)
	}
	return func(yield func(jsontext.Value, error) bool) {
		var runsOfEachBlock []iter.Seq2[jsontext.Value, error]
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
			runsOfEachBlock = append(runsOfEachBlock, runsOf(ctx, meshStack, buildingBlock.Metadata.Uuid, pageSize))
		}
		mergeNewestFirst(runsOfEachBlock)(yield)
	}
}

func runsOf(ctx context.Context, meshStack client.Client, buildingBlockUuid string, pageSize int) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
		filter := client.MeshBuildingBlockRunListFilter{BuildingBlockUuid: buildingBlockUuid}
		for run, err := range meshStack.Raw.List[client.MeshBuildingBlockRun](ctx, filter, client.ListOptions{PageSize: pageSize}) {
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
func mergeNewestFirst(runs []iter.Seq2[jsontext.Value, error]) iter.Seq2[jsontext.Value, error] {
	return func(yield func(jsontext.Value, error) bool) {
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
				yield(nil, err)
				return
			} else if ok {
				heads = append(heads, head)
			}
		}
		for len(heads) > 0 {
			newest := 0
			for i, head := range heads {
				if head.createdAt.After(heads[newest].createdAt) {
					newest = i
				}
			}
			if !yield(heads[newest].run, nil) {
				return
			}
			if ok, err := heads[newest].advance(); err != nil {
				yield(nil, err)
				return
			} else if !ok {
				heads = slices.Delete(heads, newest, newest+1)
			}
		}
	}
}

type runHead struct {
	next      func() (jsontext.Value, error, bool)
	run       jsontext.Value
	createdAt time.Time
}

func (h *runHead) advance() (bool, error) {
	run, err, ok := h.next()
	if !ok || err != nil {
		return false, err
	}
	var created struct {
		Metadata client.MeshBuildingBlockRunMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(run, &created); err != nil {
		return false, fmt.Errorf("reading the metadata.createdAt of a building block run: %w", err)
	}
	h.run, h.createdAt = run, created.Metadata.CreatedAt
	return true, nil
}
