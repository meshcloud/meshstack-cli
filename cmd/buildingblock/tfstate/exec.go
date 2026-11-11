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

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
)

// runLockTimeout must match stateLockTimeout in ../building-block-runner/internal/executor/tf/tfcmd.go.
const runLockTimeout = 5 * time.Minute

type mode string

const (
	modeRead      mode = "read"
	modeReadWrite mode = "readwrite"
)

const (
	modeFlagName            = "mode"
	forceFlagName           = "force"
	overrideBackendFlagName = "override-backend"
	backupDirFlagName       = "backup-dir"
)

var readWriteMode = fmt.Sprintf("--%s %s", modeFlagName, modeReadWrite)

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
		buildingBlockUuid string
		command           []string
		access            = modeRead
		force             bool
		backupDir         string
		overrideBackend   bool
	)

	cmd := &cobra.Command{
		Use:   "exec <building-block-uuid> -- <command> [args...]",
		Short: "Run tofu against the state of a building block",
		Long: fmt.Sprintf(`Run a command, such as tofu plan, against the state of a building block. Give the building block by its uuid.

exec passes the state to tofu's http backend through the TF_HTTP_* variables. It exits with the exit code of the command. By default, it uses the state stored in the building block's own workspace, metadata.ownedByWorkspace. Use --%[1]s to choose another workspace.

The module needs a backend "http" block. The runner adds this block only during its own runs. With --%[2]s, exec adds the block while the command runs: it writes the file %[3]s into the directory given by tofu -chdir, or into the working directory if there is no -chdir. In that directory, the file also replaces any backend that the module defines. exec removes the file when the command ends. If the command is a wrapper that runs tofu, run it in the module's directory. Run tofu init through exec with --%[2]s once.

tofu's meshstack provider also connects to meshStack through exec, with the login of the meshStack CLI. This works with any version of the provider, as long as its provider "meshstack" block sets no endpoint and no credentials. Settings in that block take precedence over the ones exec provides.

--%[4]s %[5]s is the default. In this mode, exec refuses to store a state, and the provider can only read. exec takes no lock, so a run of the building block never has to wait for it. A plan may read a state that a run is changing at the same time.

%[6]s stores what the command writes, and locks the state in meshStack whenever tofu asks for a lock. A run of the building block waits up to 5 minutes for that lock, and then fails. Without --%[7]s, %[6]s fails in these cases:
  - a run of the building block is pending, in progress or waiting for approval;
  - the new state has another lineage than the stored state, or its serial is not higher.

With --%[7]s, you can stop a destructive run that someone approved by mistake: while tofu holds the lock, the run waits for it and then fails.

With --%[8]s, exec copies the stored state into that directory before it replaces or deletes it.`,
			internal.WorkspaceFlag.Name, overrideBackendFlagName, tfstate.BackendOverrideFile, modeFlagName, modeRead, readWriteMode, forceFlagName, backupDirFlagName),
		Example: `  meshstack bb tfstate exec 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --mode readwrite -- tofu apply`,
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 || len(args) < 2 {
				return fmt.Errorf("%s needs the building block uuid, then --, then the command to run, such as '%s <building-block-uuid> -- tofu plan'",
					cmd.CommandPath(), cmd.CommandPath())
			}
			command, buildingBlockUuid = args[1:], args[0]
			return internal.UuidArg(cmd, args[:1])
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
			api := internal.ResolveClientOptions().HttpClient()
			var problems []error
			proxy := &tfstate.Proxy{
				Store:    store,
				Endpoint: session.CurrentProfile.Endpoint.URL,
				Api:      api.WithAuthorization(session),
				Writable: access == modeReadWrite,
				OnProblem: func(ctx context.Context, err error) {
					switch {
					case errors.Is(err, tfstate.ErrReadOnly):
						err = fmt.Errorf("%w, run again with %s to let the command store it", err, readWriteMode)
					case errors.Is(err, tfstate.ErrReadOnlyApi):
						err = fmt.Errorf("%w, run again with %s to let the command change meshStack", err, readWriteMode)
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
						if running := tfstate.NoRunOf(ctx, meshStack.BuildingBlockV2, buildingBlockUuid); running != nil {
							return fmt.Errorf("%w: wait for it to finish, or run again with --%s", running, forceFlagName)
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
				remove, overrideErr := tfstate.WriteBackendOverride(tfstate.ModuleDir(command))
				if overrideErr != nil {
					return fmt.Errorf("%w: remove it, or run without --%s", overrideErr, overrideBackendFlagName)
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
				slog.WarnContext(ctx, fmt.Sprintf("%s did not request any state, although --%s added the backend in %s. If the command is a wrapper that runs tofu, run it in the module's directory",
					command[0], overrideBackendFlagName, tfstate.ModuleDir(command)))
			default:
				slog.WarnContext(ctx, fmt.Sprintf("%s did not request any state. The module probably has no backend \"http\" block, because the runner adds one only during its own runs. "+
					"Run again with --%[4]s, and run '%[2]s %[3]s --%[4]s -- tofu init' once",
					command[0], cmd.CommandPath(), buildingBlockUuid, overrideBackendFlagName))
			}
			return exitWith(cmd, ran, withRightsHint(errors.Join(problems...)))
		},
	}

	flags := cmd.Flags()
	flags.Var(&access, modeFlagName, fmt.Sprintf("%s: only read the state (default); %s: also store what the command writes", modeRead, modeReadWrite))
	flags.BoolVar(&force, forceFlagName, false, "with "+readWriteMode+", store the state even while a run of the building block is pending, in progress or waiting for approval, or when the new state does not follow the stored state")
	flags.BoolVar(&overrideBackend, overrideBackendFlagName, false, "while the command runs, add a backend \"http\" block to the module in tofu's -chdir directory or in the working directory")
	flags.StringVar(&backupDir, backupDirFlagName, "", "with "+readWriteMode+", copy the stored state into this directory before each write; only you can read the copies, because a state can hold secrets")

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
