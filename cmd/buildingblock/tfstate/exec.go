package tfstate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"uuid"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
)

type mode string

const (
	modeRead      mode = "read"
	modeReadWrite mode = "readwrite"
)

func (m *mode) String() string {
	return string(*m)
}

func (m *mode) Set(value string) error {
	switch mode(value) {
	case modeRead, modeReadWrite:
		*m = mode(value)
		return nil
	}
	return fmt.Errorf("%q is neither %s nor %s", value, modeRead, modeReadWrite)
}

func (m *mode) Type() string {
	return "mode"
}

func newExec() *cobra.Command {
	var (
		buildingBlockUuid uuid.UUID
		command           []string
		access            = modeRead
		force             bool
	)

	cmd := &cobra.Command{
		Use:   "exec <building-block-uuid> -- <command> [args...]",
		Short: "Run tofu against the state of a building block",
		Long: `Run a command, such as tofu plan, against the state of a building block, named by its uuid.

exec serves the state to tofu's http backend through TF_HTTP_ADDRESS, TF_HTTP_USERNAME and
TF_HTTP_PASSWORD, and exits with the exit code of the command. The module needs a backend "http"
block: "meshstack buildingblock tfstate --help" shows the file to add. The state is the one stored
under the workspace of the building block, metadata.ownedByWorkspace, unless --workspace names
another one.

--mode read, the default, refuses to store a state. --mode readwrite stores what the command writes,
and since meshStack keeps no lock on the state, it refuses
  - while a run of the building block is pending or in progress, unless --force;
  - a state whose lineage is not that of the stored state, or whose serial is not above it.
Before it replaces or deletes the stored state, it copies it to tfstate-backups in the configuration
directory.`,
		Example: `  meshstack bb tfstate exec 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --mode readwrite -- tofu apply`,
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 || len(args) < 2 {
				return fmt.Errorf("%s takes the building block uuid, then -- and the command to run, such as `%s <building-block-uuid> -- tofu plan`",
					cmd.CommandPath(), cmd.CommandPath())
			}
			command = args[1:]
			return internal.UuidArg(&buildingBlockUuid)(cmd, args[:1])
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			store, err := openStore(ctx, meshStack, buildingBlockUuid)
			if err != nil {
				return err
			}
			var problems []error
			proxy := &tfstate.Proxy{
				Store:    store,
				Writable: access == modeReadWrite,
				OnProblem: func(ctx context.Context, err error) {
					if errors.Is(err, tfstate.ErrReadOnly) {
						err = fmt.Errorf("%w, run again with --mode readwrite to let the command store it", err)
					}
					if !slices.ContainsFunc(problems, func(problem error) bool { return problem.Error() == err.Error() }) {
						slog.WarnContext(ctx, err.Error())
						problems = append(problems, err)
					}
				},
			}
			if proxy.Writable {
				if !force {
					proxy.BeforeWrite = func(ctx context.Context) error { return noRunOf(ctx, meshStack.Raw, buildingBlockUuid) }
					if err = proxy.BeforeWrite(ctx); err != nil {
						return err
					}
				}
				if proxy.Backups, err = tfstate.ResolveBackups(ctx, internal.SettingSources()); err != nil {
					return err
				}
			}

			var ran error
			requests, err := proxy.Serve(ctx, func(env []string) { ran = run(ctx, cmd, command, env) })
			if err != nil {
				problems = append(problems, err)
			}
			if requests == 0 {
				slog.WarnContext(ctx, fmt.Sprintf("%s asked for no state, so the module likely has no backend \"http\" block, which the runner adds only while it runs. "+
					"Add the file meshstack_backend.tf with\n\n  %s\n\nto the module, and run `%s %s -- tofu init` once",
					command[0], backendFile, cmd.CommandPath(), buildingBlockUuid))
			}
			return exitWith(cmd, ran, withApiKeyHint(errors.Join(problems...)))
		},
	}

	flags := cmd.Flags()
	flags.Var(&access, "mode", "read serves the state, readwrite stores what the command writes as well")
	flags.BoolVar(&force, "force", false, "store the state even while a run of the building block is pending or in progress")

	return cmd
}

func noRunOf(ctx context.Context, raw *client.RawClient, buildingBlockUuid uuid.UUID) error {
	block, err := readBuildingBlock(ctx, raw, buildingBlockUuid)
	if err != nil {
		return err
	}
	if block.hasUnfinishedRun() {
		return fmt.Errorf("building block %s has a run %s, which writes the state as well: wait for it to finish, or run again with --force",
			buildingBlockUuid, block.Status.Status)
	}
	return nil
}

func run(ctx context.Context, cmd *cobra.Command, command, proxyEnv []string) error {
	// Ctrl-C ends ctx, and the command then still has to store what it has done.
	child := exec.CommandContext(context.WithoutCancel(ctx), command[0], command[1:]...) //nolint:gosec // G204: running the command it was given is what exec is for
	child.Env = append(os.Environ(), proxyEnv...)
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, forwardedSignals(cmd.InOrStdin())...)
	defer signal.Stop(signals)
	if err := child.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() {
		for {
			select {
			case received := <-signals:
				_ = child.Process.Signal(received)
			case <-exited:
				return
			}
		}
	}()
	err := child.Wait()
	close(exited)
	return err
}

// forwardedSignals leaves out the Ctrl-C of a terminal, because the terminal sends it to the command
// as well. tofu takes a second interrupt as a request to stop at once, without storing the state.
func forwardedSignals(stdin io.Reader) []os.Signal {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(file.Fd()) {
		return []os.Signal{syscall.SIGTERM}
	}
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// exitWith stays silent where nothing else went wrong, since the command has said why on its stderr.
func exitWith(cmd *cobra.Command, ran, problem error) error {
	exited, ok := errors.AsType[*exec.ExitError](ran)
	switch {
	case ok:
		cmd.SilenceErrors = problem == nil
		// ExitCode is -1 for a command a signal ended.
		return internal.ExitError{Code: max(exited.ExitCode(), 1), Err: problem}
	case ran != nil:
		return errors.Join(ran, problem)
	default:
		return problem
	}
}
