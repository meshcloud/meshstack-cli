package testacc

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noState = "has no state in workspace"

// TestAccTfstate runs real tofu against the state of a building block that it bootstraps itself,
// because the building blocks of the local stack are manual ones and never reach the tf-block-runner.
func TestAccTfstate(t *testing.T) {
	endpoint := requireLocalStack(t)
	_, err := exec.LookPath("tofu")
	require.NoError(t, err, "the tfstate tests run tofu, so it has to be on the PATH")
	manager := loginToBindAsWorkspaceManager(t, devLogins(t))

	c := newCLI(t, endpoint).withApiKey()
	output, err := c.run("1\n", "login", "--apikey")
	require.NoErrorf(t, err, "the API key login did not finish:\n%s", output)
	block := bootstrapBuildingBlock(t, c, manager.Username)
	c.setEnv(envWorkspace, block.workspace)
	module := copyTestdata(t, "module")

	t.Run("show writes the state that the run stored", func(t *testing.T) {
		output, err := c.run("", "buildingblock", "tfstate", "show", block.uuid)
		require.NoErrorf(t, err, "the show failed:\n%s", output)
		assert.Contains(t, output, `"terraform_data"`)
	})

	t.Run("exec --mode read runs tofu against the state", func(t *testing.T) {
		output := c.exec(t, block, "read", "tofu", "-chdir="+module, "init", "-input=false")
		assert.Contains(t, output, "Successfully configured the backend \"http\"")

		output = c.exec(t, block, "read", "tofu", "-chdir="+module, "state", "list")
		assert.Contains(t, output, "terraform_data.noop")
		c.exec(t, block, "read", "tofu", "-chdir="+module, "plan", "-input=false")
	})

	t.Run("exec --mode readwrite applies with tofu, and leaves no lock behind", func(t *testing.T) {
		output := c.exec(t, block, "readwrite", "tofu", "-chdir="+module, "apply", "-auto-approve", "-input=false")
		assert.Contains(t, output, "Apply complete!")
		assert.Contains(t, c.forceUnlock(t, block), "has no lock")
	})

	t.Run("a second exec fails on the lock that the first one holds, and names it", func(t *testing.T) {
		release := c.holdLock(t, block, "held-by-the-first-exec")

		output, err := c.run("", "buildingblock", "tfstate", "exec", block.uuid, "--mode", "readwrite", "--",
			"tofu", "-chdir="+module, "apply", "-auto-approve", "-input=false", "-lock-timeout=1s")
		require.Errorf(t, err, "tofu applied while another exec held the lock:\n%s", output)
		assert.Contains(t, output, "Error acquiring the state lock")
		assert.Contains(t, output, "held-by-the-first-exec")

		c.exec(t, block, "read", "tofu", "-chdir="+module, "plan", "-input=false")
		release(t)
	})

	t.Run("force-unlock releases the lock that an exec left behind", func(t *testing.T) {
		output, err := c.run("", "buildingblock", "tfstate", "exec", block.uuid, "--mode", "readwrite", "--",
			"sh", "-ec", `curl --silent --show-error --fail --user "$TF_HTTP_USERNAME:$TF_HTTP_PASSWORD" --data '`+tofuLockInfo("left-behind")+`' "$TF_HTTP_LOCK_ADDRESS"`)
		require.NoErrorf(t, err, "the exec could not lock the state:\n%s", output)

		output, err = c.run("no\n", "buildingblock", "tfstate", "force-unlock", block.uuid)
		require.Errorf(t, err, "the force-unlock released the lock without a yes:\n%s", output)
		assert.Contains(t, output, "Release the lock left-behind?")
		assert.Contains(t, err.Error(), "kept the lock left-behind")

		output = c.forceUnlock(t, block)
		assert.Contains(t, output, "lock ID left-behind")
		assert.Contains(t, output, "Released the lock left-behind")
		assert.Contains(t, c.forceUnlock(t, block), "has no lock")
	})

	t.Run("concurrent execs take turns on the lock, and none of their writes is lost", func(t *testing.T) {
		const execs = 4
		before := c.storedVersion(t, block)
		runs := make([]*cliRun, execs)
		for i := range runs {
			runs[i] = c.start("", "buildingblock", "tfstate", "exec", block.uuid, "--mode", "readwrite", "--",
				"tofu", "-chdir="+module, "apply", "-auto-approve", "-input=false", "-lock-timeout=2m", "-var", fmt.Sprintf("input=exec-%d", i))
		}
		for i, run := range runs {
			require.NoErrorf(t, run.wait(), "exec %d failed:\n%s", i, run.output.String())
		}

		after := c.storedVersion(t, block)
		assert.Equal(t, before.Lineage, after.Lineage)
		assert.Equal(t, before.Serial+execs, after.Serial, "every apply stores one state, on top of the one the apply before stored")
		assert.Contains(t, c.forceUnlock(t, block), "has no lock")
	})

	t.Run("a run waits for the lock that an exec holds, and succeeds once the exec releases it", func(t *testing.T) {
		release := c.holdLock(t, block, "held-while-a-run-waits")
		runUuid := c.triggerRun(t, block)
		c.awaitRunStatus(t, block, "IN_PROGRESS")
		// Long enough for the noop run to have finished, had it not waited for the lock. The runner
		// waits up to 5 minutes before it fails the run.
		<-time.After(90 * time.Second)
		assert.Equal(t, "IN_PROGRESS", c.latestRunStatus(t, block), "the run did not wait for the lock")
		release(t)

		output, err := c.run("", "buildingblockrun", "logs", runUuid, "--follow")
		require.NoErrorf(t, err, "the run did not succeed after the exec released the lock:\n%s", output)
	})

	t.Run("a browser login of a Workspace Manager reads, locks and writes the state", func(t *testing.T) {
		browser := newCLI(t, endpoint)
		browser.setEnv(envWorkspace, block.workspace)
		login := startLogin(t, browser, "")
		completeKeycloakLogin(t, login.awaitStartURL(t), "full", manager.Username, manager.Password)
		require.NoErrorf(t, login.wait(), "the browser login did not finish:\n%s", login.output.String())

		output, err := browser.run("", "buildingblock", "tfstate", "show", block.uuid)
		require.NoErrorf(t, err, "the Workspace Manager could not read the state:\n%s", output)
		output = browser.exec(t, block, "readwrite", "tofu", "-chdir="+module, "apply", "-auto-approve", "-input=false")
		assert.Contains(t, output, "Apply complete!")
	})

	// Runs last, as it deletes the state. curl stands in for tofu's http backend, so that the test
	// can send a write without the lock ID, which tofu never does.
	t.Run("exec stores, reads and deletes a state with the ID of the lock it holds", func(t *testing.T) {
		const minimalState = `{"version":4,"serial":1,"lineage":"meshstack-cli-testacc"}`
		const script = `state() { curl --silent --show-error --fail --user "$TF_HTTP_USERNAME:$TF_HTTP_PASSWORD" "$@"; }
state --data '` + `LOCK_INFO` + `' "$TF_HTTP_LOCK_ADDRESS"
if state --data '` + minimalState + `' "$TF_HTTP_ADDRESS" 2>/dev/null; then echo "stored without the lock ID"; fi
state --data '` + minimalState + `' "$TF_HTTP_ADDRESS?ID=the-test-lock" >/dev/null
echo "read $(state "$TF_HTTP_ADDRESS")"
state --request DELETE "$TF_HTTP_ADDRESS?ID=the-test-lock"
state --request DELETE --data '` + `LOCK_INFO` + `' "$TF_HTTP_UNLOCK_ADDRESS"`
		output, err := c.run("", "buildingblock", "tfstate", "exec", block.uuid, "--mode", "readwrite", "--force", "--",
			"sh", "-ec", strings.ReplaceAll(script, "LOCK_INFO", tofuLockInfo("the-test-lock")))
		require.NoErrorf(t, err, "the exec failed:\n%s", output)
		assert.NotContains(t, output, "stored without the lock ID")
		assert.Contains(t, output, "read "+minimalState, "the GET reads the state the POST stored")
		assert.Contains(t, output, "Saved the stored state of building block "+block.uuid, "the POST backs the state up first")

		output, err = c.run("", "buildingblock", "tfstate", "show", block.uuid)
		require.NoErrorf(t, err, "the show failed:\n%s", output)
		assert.Contains(t, output, noState, "the DELETE through the proxy removed the state")
		assert.Contains(t, c.forceUnlock(t, block), "has no lock")
	})
}

type bootstrappedBlock struct {
	uuid, workspace string
}

// loginToBindAsWorkspaceManager leaves out an Organization Admin, whose rights reach every state
// without the role.
func loginToBindAsWorkspaceManager(t *testing.T, logins []devLogin) devLogin {
	t.Helper()
	found := slices.IndexFunc(logins, func(login devLogin) bool {
		return !slices.Contains(slices.Collect(maps.Values(login.Workspaces)), "Organization Admin")
	})
	require.NotEqualf(t, -1, found, "%s carries no login that is no Organization Admin", envTestUsers)
	return logins[found]
}

func bootstrapBuildingBlock(t *testing.T, c *cli, workspaceManager string) bootstrappedBlock {
	t.Helper()
	runId := "cli-" + strings.ToLower(rand.Text()[:8])
	dir := copyTestdata(t, "bootstrap")
	vars := []string{"-var", "profile=" + defaultProfile, "-var", "run_id=" + runId, "-var", "workspace_manager=" + workspaceManager}

	c.tofu(t, dir, append([]string{"init", "-input=false"}, vars...)...)
	t.Cleanup(func() {
		if output, err := c.tofuCommand(t, dir, append([]string{"destroy", "-auto-approve", "-input=false"}, vars...)...).CombinedOutput(); err != nil {
			t.Errorf("tofu destroy left the bootstrapped objects of %s behind:\n%s", runId, output)
		}
	})
	c.tofu(t, dir, append([]string{"apply", "-auto-approve", "-input=false"}, vars...)...)

	in := *c
	in.extraEnv = slices.Clone(c.extraEnv)
	in.setEnv(envWorkspace, runId)
	output, err := in.run("", "buildingblock", "list", "--limit", "unlimited", "-o", "ndjson")
	require.NoErrorf(t, err, "the building block list failed:\n%s", output)
	for _, listed := range ndjsonObjects[struct {
		Metadata struct {
			Uuid string `json:"uuid"`
		} `json:"metadata"`
		Spec struct {
			DisplayName string `json:"displayName"`
		} `json:"spec"`
	}](output) {
		if listed.Spec.DisplayName == runId+"-noop" {
			return bootstrappedBlock{uuid: listed.Metadata.Uuid, workspace: runId}
		}
	}
	t.Fatalf("workspace %s holds no building block %s-noop:\n%s", runId, runId, output)
	return bootstrappedBlock{}
}

// copyTestdata keeps what tofu writes, .terraform and the local state, out of the source tree.
func copyTestdata(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.CopyFS(dir, os.DirFS(filepath.Join("testdata", name))))
	return dir
}

// tofuCommand runs tofu in the environment of c, so that the provider reads the profile that c
// logged in to.
func (c *cli) tofuCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "tofu", append([]string{"-chdir=" + dir}, args...)...)
	cmd.Env = append(os.Environ(), c.environ()...)
	return cmd
}

func (c *cli) tofu(t *testing.T, dir string, args ...string) {
	t.Helper()
	output, err := c.tofuCommand(t, dir, args...).CombinedOutput()
	require.NoErrorf(t, err, "tofu %s failed:\n%s", args[0], output)
}

func (c *cli) exec(t *testing.T, block bootstrappedBlock, mode string, command ...string) string {
	t.Helper()
	output, err := c.run("", append([]string{"buildingblock", "tfstate", "exec", block.uuid, "--mode", mode, "--"}, command...)...)
	require.NoErrorf(t, err, "the exec of %s failed:\n%s", strings.Join(command, " "), output)
	return output
}

type stateVersion struct {
	Lineage string `json:"lineage"`
	Serial  int    `json:"serial"`
}

func (c *cli) storedVersion(t *testing.T, block bootstrappedBlock) (version stateVersion) {
	t.Helper()
	show := c.start("", "buildingblock", "tfstate", "show", block.uuid)
	require.NoErrorf(t, show.wait(), "the show failed:\n%s", show.output.String())
	require.NoError(t, json.Unmarshal([]byte(show.stdout.String()), &version))
	return version
}

func (c *cli) forceUnlock(t *testing.T, block bootstrappedBlock) string {
	t.Helper()
	output, err := c.run("yes\n", "buildingblock", "tfstate", "force-unlock", block.uuid)
	require.NoErrorf(t, err, "the force-unlock failed:\n%s", output)
	return output
}

func tofuLockInfo(id string) string {
	return `{"ID":"` + id + `","Operation":"OperationTypeApply","Info":"","Who":"testacc","Version":"1.12.0","Created":"2026-01-01T00:00:00Z","Path":""}`
}

func (c *cli) holdLock(t *testing.T, block bootstrappedBlock, id string) (release func(t *testing.T)) {
	t.Helper()
	released := filepath.Join(t.TempDir(), "released")
	script := `state() { curl --silent --show-error --fail --user "$TF_HTTP_USERNAME:$TF_HTTP_PASSWORD" "$@"; }
state --data '` + tofuLockInfo(id) + `' "$TF_HTTP_LOCK_ADDRESS"
echo "locked ` + id + `"
while [ ! -e '` + released + `' ]; do sleep 0.2; done
state --request DELETE --data '` + tofuLockInfo(id) + `' "$TF_HTTP_UNLOCK_ADDRESS"`
	run := c.start("", "buildingblock", "tfstate", "exec", block.uuid, "--mode", "readwrite", "--", "sh", "-ec", script)
	require.Eventuallyf(t, func() bool { return strings.Contains(run.output.String(), "locked "+id) }, time.Minute, 100*time.Millisecond,
		"the exec did not lock the state:\n%s", run.output.String())
	return func(t *testing.T) {
		t.Helper()
		require.NoError(t, os.WriteFile(released, nil, 0o600))
		require.NoErrorf(t, run.wait(), "the exec that held the lock failed:\n%s", run.output.String())
	}
}

func (c *cli) triggerRun(t *testing.T, block bootstrappedBlock) string {
	t.Helper()
	output, err := c.run("", "buildingblock", "trigger-run", block.uuid, "-o", "ndjson")
	require.NoErrorf(t, err, "the trigger-run failed:\n%s", output)
	written := ndjsonObjects[struct {
		Status struct {
			LatestRunUuid string `json:"latestRunUuid"`
		} `json:"status"`
	}](output)
	require.Lenf(t, written, 1, "the trigger-run wrote no building block:\n%s", output)
	return written[0].Status.LatestRunUuid
}

func (c *cli) latestRunStatus(t *testing.T, block bootstrappedBlock) string {
	t.Helper()
	output, err := c.run("", "buildingblockrun", "list", "--building-block", block.uuid, "--limit", "1", "-o", "ndjson")
	require.NoErrorf(t, err, "the run list failed:\n%s", output)
	runs := ndjsonObjects[struct {
		Status string `json:"status"`
	}](output)
	require.Lenf(t, runs, 1, "the run list listed no run:\n%s", output)
	return runs[0].Status
}

func (c *cli) awaitRunStatus(t *testing.T, block bootstrappedBlock, status string) {
	t.Helper()
	timeout := time.After(3 * time.Minute)
	for latest := c.latestRunStatus(t, block); latest != status; latest = c.latestRunStatus(t, block) {
		select {
		case <-timeout:
			t.Fatalf("the run of building block %s is %s, and did not reach %s", block.uuid, latest, status)
		case <-time.After(time.Second):
		}
	}
}
