package eventlog

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const (
	fromFlagName  = "from"
	untilFlagName = "until"
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

--` + fromFlagName + ` and --` + untilFlagName + ` accept a date such as 2026-07-01, which means midnight UTC, or an instant such as 2026-07-01T12:00:00Z.

The command lists the event logs of the workspace. With an admin credential and no workspace set, it lists the event logs of all workspaces.`,
		Example: `  meshstack eventlog list --from 2026-07-01 --until 2026-07-02
  meshstack elog list --title "Building Block Run" --exclude-title "Building Block Run Executed"
  meshstack elog list --workspace my-workspace --limit unlimited -o ndjson`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !from.instant.IsZero() && !until.instant.IsZero() && !from.instant.Before(until.instant) {
				return fmt.Errorf("--%s %s is not before --%s %s, so no event log can match", fromFlagName, &from, untilFlagName, &until)
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
	cmd.Flags().Var(&from, fromFlagName, "list the event logs created at or after this date or instant")
	cmd.Flags().Var(&until, untilFlagName, "list the event logs created before this date or instant")
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
	return fmt.Errorf("%q is not a valid date or instant; use 2026-07-01 or 2026-07-01T12:00:00Z", value)
}

func (f *instantFlag) Type() string {
	return "instant"
}
