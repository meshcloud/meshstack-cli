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
	"time"
	"uuid"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
)

// runLockTimeout must match stateLockTimeout in ../building-block-runner/tf-block-runner/tfrun/tfcmd.go.
const runLockTimeout = 5 * time.Minute

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
		backupDir         string
		overrideBackend   bool
	)

	cmd := &cobra.Command{
		Use:   "exec <building-block-uuid> -- <command> [args...]",
		Short: "Run tofu against the state of a building block",
		Long: `Run a command, such as tofu plan, against the state of a building block, named by its uuid.

exec serves the state to tofu's http backend through the TF_HTTP_* variables, and exits with the
exit code of the command. The state is the one stored under the workspace of the building block,
metadata.ownedByWorkspace, unless --workspace names another one.

The module needs a backend "http" block, which the runner adds only while it runs.
--override-backend adds one while the command runs: it writes the file
` + tfstate.BackendOverrideFile + ` into the working directory, where it also replaces a backend
of the module's own, and removes the file when the command ends. So run exec in the module's
directory rather than with tofu -chdir, and run tofu init through exec with --override-backend once.

tofu's meshstack provider talks to meshStack through exec as well, with the login of the CLI. This
works with any version of the provider, as long as its provider "meshstack" block sets no endpoint
and no credentials, which would win over what exec gives it.

--mode read, the default, refuses to store a state, and lets the provider only read. It takes no
lock, so it never makes a run of the building block wait, and a plan may read a state that a run is
changing.

--mode readwrite stores what the command writes, and locks the state in meshStack whenever tofu
asks for a lock. A run of the building block waits for that lock for up to 5 minutes, and then
fails. Unless --force, --mode readwrite refuses
  - while a run of the building block is pending or in progress;
  - a state whose lineage is not that of the stored state, or whose serial is not above it.
With --backup-dir, it copies the stored state into that directory before it replaces or deletes it.`,
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
			session, err := internal.ResolveSession(ctx)
			if err != nil {
				return err
			}
			meshStack, err := session.Client()
			if err != nil {
				return err
			}
			store, err := openStore(ctx, meshStack, buildingBlockUuid)
			if err != nil {
				return err
			}
			api, err := internal.ResolveClientOptions().HttpClient()
			if err != nil {
				return err
			}
			var problems []error
			proxy := &tfstate.Proxy{
				Store:    store,
				Endpoint: session.CurrentProfile.Endpoint.URL,
				Api:      api.WithAuthorization(session),
				Writable: access == modeReadWrite,
				OnProblem: func(ctx context.Context, err error) {
					switch {
					case errors.Is(err, tfstate.ErrReadOnly):
						err = fmt.Errorf("%w, run again with --mode readwrite to let the command store it", err)
					case errors.Is(err, tfstate.ErrReadOnlyApi):
						err = fmt.Errorf("%w, run again with --mode readwrite to let the command change meshStack", err)
					}
					if !slices.ContainsFunc(problems, func(problem error) bool { return problem.Error() == err.Error() }) {
						slog.WarnContext(ctx, err.Error())
						problems = append(problems, err)
					}
				},
			}
			if proxy.Writable {
				proxy.Force = force
				proxy.LockWarning = runLockTimeout
				if !force {
					proxy.BeforeWrite = func(ctx context.Context) error {
						if running := tfstate.NoRunOf(ctx, meshStack.Raw, buildingBlockUuid); running != nil {
							return fmt.Errorf("%w: wait for it to finish, or run again with --force", running)
						}
						return nil
					}
					if err = proxy.BeforeWrite(ctx); err != nil {
						return err
					}
				}
				proxy.Backups = tfstate.Backups(backupDir)
			}

			if overrideBackend {
				remove, overrideErr := tfstate.WriteBackendOverride(".")
				if overrideErr != nil {
					return fmt.Errorf("%w: remove it, or run without --override-backend", overrideErr)
				}
				defer func() {
					if removeErr := remove(); removeErr != nil {
						slog.WarnContext(ctx, "Cannot remove the backend override: "+removeErr.Error())
					}
				}()
			}

			var ran error
			requests, err := proxy.Serve(ctx, func(env []string) { ran = run(ctx, cmd, command, env) })
			if err != nil {
				problems = append(problems, err)
			}
			switch {
			case requests > 0:
			case overrideBackend:
				slog.WarnContext(ctx, command[0]+" asked for no state. --override-backend adds the backend in the working directory, so run exec in the module's directory rather than with tofu -chdir")
			default:
				slog.WarnContext(ctx, fmt.Sprintf("%s asked for no state, so the module likely has no backend \"http\" block, which the runner adds only while it runs. "+
					"Run again with --override-backend, and run `%s %s --override-backend -- tofu init` once",
					command[0], cmd.CommandPath(), buildingBlockUuid))
			}
			return exitWith(cmd, ran, withRightsHint(errors.Join(problems...)))
		},
	}

	flags := cmd.Flags()
	flags.Var(&access, "mode", "read serves the state, readwrite stores what the command writes as well")
	flags.BoolVar(&force, "force", false, "store the state even while a run of the building block is pending or in progress, or where it does not follow the stored state")
	flags.BoolVar(&overrideBackend, "override-backend", false, "add the backend \"http\" block to the module in the working directory while the command runs")
	flags.StringVar(&backupDir, "backup-dir", "", "with --mode readwrite, copy the stored state into this directory before each write, in files only you can read, since a state can hold secrets")

	return cmd
}

func run(ctx context.Context, cmd *cobra.Command, command, env []string) error {
	// Ctrl-C ends ctx, and the command then still has to store what it has done.
	child := exec.CommandContext(context.WithoutCancel(ctx), command[0], command[1:]...) //nolint:gosec // G204: running the command it was given is what exec is for
	child.Env = append(os.Environ(), env...)
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
