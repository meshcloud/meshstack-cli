package internal

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"log/slog"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// ListFlags are the flags every list command takes, and Run is how each one lists.
type ListFlags struct {
	output OutputFlag
	limit  LimitFlag
}

func (f *ListFlags) Register(flags *pflag.FlagSet) {
	f.output.Register(flags)
	flags.Var(&f.limit, "limit", "list at most this many items, 0 lists all of them")
}

// ListWorkspace is the workspace a listing is narrowed to, or nil for none. It leaves out the
// profile's default workspace, so that a listing asked for no workspace shows what the credential
// can see.
func ListWorkspace(ctx context.Context) (*string, error) {
	workspace, err := setting.ResolveWorkspace(ctx, SettingSources())
	if err != nil || workspace == "" {
		return nil, err
	}
	return &workspace, nil
}

func (f *ListFlags) Run(cmd *cobra.Command, list func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error]) error {
	ctx := cmd.Context()
	meshStack, err := ResolveClient(ctx)
	if err != nil {
		return err
	}
	limit := int(f.limit)
	var total *int
	listOptions := client.ListOptions{
		PageSize: limit,
		OnPage: func(page client.Page) {
			if total == nil {
				total = &page.TotalElements
			}
		},
	}
	listed := 0
	items := counted(First(list(client.WithListOptions(ctx, listOptions), meshStack), limit), &listed)
	if err := WriteList(cmd.OutOrStdout(), f.output.Format, items); err != nil {
		return err
	}
	if note := CutShortNote(limit, listed, total); note != "" {
		slog.InfoContext(ctx, note)
	}
	return nil
}

// CutShortNote says that a listing stopped at its limit before the end, and is empty for a listing
// that is complete.
func CutShortNote(limit, listed int, total *int) string {
	const howToListMore = "raise --limit, or set it to 0 to list all of them"
	switch {
	case limit == 0 || listed < limit:
		return ""
	case total == nil:
		return fmt.Sprintf("stopped at the limit of %d, there may be more; %s", limit, howToListMore)
	case *total > limit:
		return fmt.Sprintf("listed the first %d of %d; %s", limit, *total, howToListMore)
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

type LimitFlag int

func (f *LimitFlag) String() string {
	return strconv.Itoa(int(*f))
}

func (f *LimitFlag) Set(value string) error {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 {
		return fmt.Errorf("%q is no limit, write a number of items, or 0 for all of them", value)
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
