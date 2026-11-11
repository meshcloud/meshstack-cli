package internal

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/types"
)

type ListFlags struct {
	output OutputFlag
	limit  LimitFlag
}

// defaultLimit keeps a listing nobody limited from flooding a terminal.
const defaultLimit = 100

func (f *ListFlags) Register(flags *pflag.FlagSet) {
	f.output.Register(flags)
	f.limit = defaultLimit
	flags.Var(&f.limit, limitFlagName, "list at most this many items, or unlimited for all of them")
}

// ListWorkspace is the workspace the session works in, which a listing keeps to as every other
// command does, and nil where the session works in none.
func ListWorkspace(ctx context.Context) (*string, error) {
	workspace, err := ResolveWorkspace(ctx)
	if err != nil || workspace == "" {
		return nil, err
	}
	return &workspace, nil
}

func (f *ListFlags) Run[M, Filter any](cmd *cobra.Command, kind client.MeshObjectClient[M, Filter], filter Filter) error {
	var page types.Page
	return f.run(cmd, &page, func(ctx context.Context, options ...client.ListOption) iter.Seq2[any, error] {
		return WithKindAndApiVersion(kind, kind.List(ctx, filter, append(options, client.WithCapturedPage(&page))...))
	})
}

func WithKindAndApiVersion[M, Filter any](kind client.MeshObjectClient[M, Filter], objects iter.Seq2[M, error]) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for object, err := range objects {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(client.WithKindAndApiVersion(kind, object), nil) {
				return
			}
		}
	}
}

// RunSeq is Run for a listing that is not one kind's, such as the runs of every building block.
func (f *ListFlags) RunSeq(cmd *cobra.Command, list func(ctx context.Context, options ...client.ListOption) iter.Seq2[any, error]) error {
	return f.run(cmd, nil, list)
}

func (f *ListFlags) run(cmd *cobra.Command, page *types.Page, list func(ctx context.Context, options ...client.ListOption) iter.Seq2[any, error]) error {
	ctx := cmd.Context()
	result := ListResult{Limit: int(f.limit), Defaulted: !cmd.Flags().Changed(limitFlagName)}
	items := counted(First(list(ctx, client.WithNewestFirst(time.Now()), client.WithPageSize(result.Limit)), result.Limit), &result.Listed)
	if err := f.output.Format.WriteList(cmd.OutOrStdout(), items); err != nil {
		return err
	}
	if page != nil {
		result.Total = &page.TotalElements
	}
	if note := result.CutShortNote(); note != "" {
		slog.InfoContext(ctx, note)
	}
	return nil
}

type ListResult struct {
	// Limit is 0 for unlimited.
	Limit, Listed int
	Total         *int
	Defaulted     bool
}

func (r ListResult) CutShortNote() string {
	const howToListMore = "raise --limit, or pass --limit unlimited to list all of them"
	switch {
	case r.Limit == 0 || r.Listed < r.Limit:
		return ""
	case r.Total == nil && r.Defaulted:
		return fmt.Sprintf("stopped at the default limit of %d, there may be more; %s", r.Limit, howToListMore)
	case r.Total == nil:
		return fmt.Sprintf("stopped at the limit of %d, there may be more; %s", r.Limit, howToListMore)
	case *r.Total > r.Limit && r.Defaulted:
		return fmt.Sprintf("listed the first %d of %d, the default limit; %s", r.Limit, *r.Total, howToListMore)
	case *r.Total > r.Limit:
		return fmt.Sprintf("listed the first %d of %d; %s", r.Limit, *r.Total, howToListMore)
	default:
		return ""
	}
}

func counted[T any](items iter.Seq2[T, error], count *int) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for item, err := range items {
			if err == nil {
				*count++
			}
			if !yield(item, err) {
				return
			}
		}
	}
}

const limitFlagName = "limit"

type LimitFlag int

const unlimited = "unlimited"

func (f *LimitFlag) String() string {
	if *f == 0 {
		return unlimited
	}
	return strconv.Itoa(int(*f))
}

func (f *LimitFlag) Set(value string) error {
	if value == unlimited {
		*f = 0
		return nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fmt.Errorf("%q is not a valid limit; use a number of items, or unlimited for all of them", value)
	}
	*f = LimitFlag(limit)
	return nil
}

func (f *LimitFlag) Type() string {
	return "count"
}

func First[T any](items iter.Seq2[T, error], n int) iter.Seq2[T, error] {
	if n == 0 {
		return items
	}
	return func(yield func(T, error) bool) {
		yielded := 0
		for item, err := range items {
			if !yield(item, err) || err != nil {
				return
			}
			if yielded++; yielded == n {
				return
			}
		}
	}
}
