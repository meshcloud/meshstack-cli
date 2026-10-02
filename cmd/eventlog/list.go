package eventlog

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func newList() *cobra.Command {
	var (
		flags       internal.ListFlags
		title       string
		exclude     []string
		from, until instantFlag
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List event logs, newest first",
		Long: `List event logs, newest first.

--from and --until take a date such as 2026-07-01, which means midnight UTC, or an instant such as
2026-07-01T12:00:00Z.

An admin credential lists the event logs of every workspace, unless --workspace names one: the
profile's default workspace does not narrow the list. Any other credential lists those of its own
workspace.`,
		Example: `  meshstack eventlog list --from 2026-07-01 --until 2026-07-02
  meshstack elog list --title "Building Block Run" --exclude-title "Building Block Run Executed"
  meshstack elog list --workspace my-workspace --limit unlimited -o ndjson`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !from.instant.IsZero() && !until.instant.IsZero() && !from.instant.Before(until.instant) {
				return fmt.Errorf("--from %s is not before --until %s, so no event log can match", &from, &until)
			}
			workspace, err := internal.ListWorkspace(cmd.Context())
			if err != nil {
				return err
			}
			filter := client.MeshEventLogListFilter{
				From:         from.instant,
				Until:        until.instant,
				Title:        title,
				ExcludeTitle: exclude,
			}
			if workspace != nil {
				filter.WorkspaceIdentifier = *workspace
			}
			return flags.Run[client.MeshEventLog](cmd, filter)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "list the event logs whose title contains this text")
	cmd.Flags().StringArrayVar(&exclude, "exclude-title", nil, "leave out the event logs of exactly this title")
	cmd.Flags().Var(&from, "from", "list the event logs created at or after this date or instant")
	cmd.Flags().Var(&until, "until", "list the event logs created before this date or instant")
	flags.Register(cmd.Flags())

	return cmd
}

// instantFlag implements [pflag.Value], so a malformed date fails while the flags are parsed.
type instantFlag struct{ instant time.Time }

func (f *instantFlag) String() string {
	if f.instant.IsZero() {
		return ""
	}
	return f.instant.Format(time.RFC3339)
}

func (f *instantFlag) Set(value string) error {
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if instant, err := time.Parse(layout, value); err == nil {
			f.instant = instant
			return nil
		}
	}
	return fmt.Errorf("%q is no date or instant, write 2026-07-01 or 2026-07-01T12:00:00Z", value)
}

func (f *instantFlag) Type() string {
	return "instant"
}
