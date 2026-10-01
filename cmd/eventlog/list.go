package eventlog

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		flags       internal.ListFlags
		title       string
		from, until instantFlag
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List event logs, oldest first",
		Long: `List event logs, oldest first.

--title keeps the event logs whose title contains the text, so compare the title yourself if you
need an exact match. --from includes its instant and --until excludes its instant. Both take a
date such as 2026-07-01, which means midnight UTC, or an instant such as 2026-07-01T12:00:00Z.

A credential with the admin permission to list event logs lists the event logs of every workspace,
and --workspace, or MESHSTACK_WORKSPACE, narrows the list to one workspace. Any other credential
lists the event logs of its own workspace only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			filter := client.MeshEventLogListFilter{From: from.instant, Until: until.instant}
			if title != "" {
				filter.Title = &title
			}
			var err error
			if filter.WorkspaceIdentifier, err = internal.ListWorkspace(cmd.Context()); err != nil {
				return err
			}
			return flags.Run(cmd, func(ctx context.Context, meshStack client.Client) iter.Seq2[jsontext.Value, error] {
				return meshStack.Listing.EventLogs(ctx, filter)
			})
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "list the event logs whose title contains this text")
	cmd.Flags().Var(&from, "from", "list the event logs created at or after this date or instant")
	cmd.Flags().Var(&until, "until", "list the event logs created before this date or instant")
	flags.Register(cmd.Flags())

	return cmd
}

// instantFlag implements [pflag.Value], so a malformed date fails while the flags are parsed.
type instantFlag struct{ instant *time.Time }

func (f *instantFlag) String() string {
	if f.instant == nil {
		return ""
	}
	return f.instant.Format(time.RFC3339)
}

func (f *instantFlag) Set(value string) error {
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if instant, err := time.Parse(layout, value); err == nil {
			f.instant = &instant
			return nil
		}
	}
	return fmt.Errorf("%q is no date or instant, write 2026-07-01 or 2026-07-01T12:00:00Z", value)
}

func (f *instantFlag) Type() string {
	return "instant"
}
