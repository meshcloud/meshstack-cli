package buildingblocktfstate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblocktfstate"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
	buildingBlockPath = "/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid
	state             = `{"version":4,"serial":1,"lineage":"lineage-1","resources":[]}`
	childSteps        = "MESHSTACK_TFSTATE_TEST_CHILD"
)

func statePath(workspace string) string {
	return "/api/terraform/state/workspace/" + workspace + "/buildingBlock/" + buildingBlockUuid
}

// TestMain runs the steps of childSteps where exec started the test binary: GET asks the proxy for
// the state, POST stores state, and exit=<code> exits.
func TestMain(m *testing.M) {
	if steps, ok := os.LookupEnv(childSteps); ok {
		os.Exit(runChild(steps))
	}
	os.Exit(m.Run())
}

func runChild(steps string) int {
	for step := range strings.FieldsSeq(steps) {
		if code, ok := strings.CutPrefix(step, "exit="); ok {
			exitCode, _ := strconv.Atoi(code)
			return exitCode
		}
		address, _ := url.Parse(os.Getenv("TF_HTTP_ADDRESS"))
		r := httptest.NewRequestWithContext(context.Background(), step, "/", nil)
		r.SetBasicAuth(os.Getenv("TF_HTTP_USERNAME"), os.Getenv("TF_HTTP_PASSWORD"))
		_, _ = http.NewClient("").DoRequest[[]byte](context.Background(), step, address, http.WithHeaders(r.Header), http.WithBody([]byte(state)))
	}
	return 0
}

type fakeMeshStack struct {
	blockStatus     string
	refuseStateWith int
	requests        []string
}

func newFakeMeshStack(t *testing.T) *fakeMeshStack {
	t.Helper()
	f := &fakeMeshStack{blockStatus: "SUCCEEDED"}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	testlogin.LoggedInTo(t, server.URL)
	return f
}

func (f *fakeMeshStack) ServeHTTP(w gohttp.ResponseWriter, r *gohttp.Request) {
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == buildingBlockPath:
		_, _ = fmt.Fprintf(w, `{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, "status": {"status": %q}}`,
			buildingBlockUuid, f.blockStatus)
	case strings.HasPrefix(r.URL.Path, "/api/terraform/state/") && f.refuseStateWith != 0:
		w.WriteHeader(f.refuseStateWith)
	case r.URL.Path == statePath("my-workspace") || r.URL.Path == statePath("other-workspace"):
		_, _ = io.WriteString(w, state)
	default:
		gohttp.NotFound(w, r)
	}
}

type result struct {
	stdout, stderr, log string
	err                 error
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr, log bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	cmd := buildingblocktfstate.New()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return result{stdout.String(), stderr.String(), log.String(), err}
}

func execChild(t *testing.T, steps string, args ...string) result {
	t.Helper()
	t.Setenv(childSteps, steps)
	testBinary, err := os.Executable()
	require.NoError(t, err)
	return run(t, append(append([]string{"exec", buildingBlockUuid}, args...), "--", testBinary)...)
}

func TestShowWritesTheStateAsItCame(t *testing.T) {
	meshStack := newFakeMeshStack(t)

	shown := run(t, "show", buildingBlockUuid)

	require.NoError(t, shown.err)
	assert.Equal(t, state, shown.stdout, "the state as it came, not merely the same JSON") //nolint:testifylint // encoded-compare: JSONEq would take the state reformatted
	assert.Equal(t, []string{"GET " + buildingBlockPath, "GET " + statePath("my-workspace")}, meshStack.requests,
		"the state is stored under the workspace of the building block")
}

func TestShowTakesTheStateOfTheWorkspaceGiven(t *testing.T) {
	meshStack := newFakeMeshStack(t)
	t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "other-workspace")

	shown := run(t, "show", buildingBlockUuid)

	require.NoError(t, shown.err)
	assert.Equal(t, []string{"GET " + statePath("other-workspace")}, meshStack.requests)
}

func TestShowOfABuildingBlockWithoutStateWritesNothing(t *testing.T) {
	meshStack := newFakeMeshStack(t)
	meshStack.refuseStateWith = gohttp.StatusNotFound

	shown := run(t, "show", buildingBlockUuid)

	require.NoError(t, shown.err)
	assert.Empty(t, shown.stdout)
	assert.Contains(t, shown.log, "has no state in workspace my-workspace yet")
}

func TestARefusalOfMeshStackSaysThatTheStateTakesAnApiKey(t *testing.T) {
	tests := map[string]func(t *testing.T) error{
		"show": func(t *testing.T) error {
			t.Helper()
			return run(t, "show", buildingBlockUuid).err
		},
		"exec": func(t *testing.T) error {
			t.Helper()
			return execChild(t, "GET").err
		},
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			newFakeMeshStack(t).refuseStateWith = gohttp.StatusForbidden

			err := command(t)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "http error 403")
			assert.Contains(t, err.Error(), "This command needs an API key login, meshstack login --apikey")
			assert.Contains(t, err.Error(), "TFSTATE_LIST")
		})
	}
}

func TestExecExitsWithTheExitCodeOfTheCommand(t *testing.T) {
	newFakeMeshStack(t)

	ran := execChild(t, "GET exit=3")

	exitErr, ok := errors.AsType[internal.ExitError](ran.err)
	require.True(t, ok, "%v", ran.err)
	assert.Equal(t, 3, exitErr.Code)
	assert.Empty(t, ran.stderr, "the command has said why it failed")
}

func TestExecWarnsWhereTheCommandAskedForNoState(t *testing.T) {
	newFakeMeshStack(t)

	ran := execChild(t, "exit=0")

	require.NoError(t, ran.err)
	assert.Contains(t, ran.log, `asked for no state, so the module likely has no backend \"http\" block`)
	assert.Contains(t, ran.log, "meshstack_backend.tf")
}

func TestExecOfAStateItAskedForWarnsOfNothing(t *testing.T) {
	newFakeMeshStack(t)

	ran := execChild(t, "GET")

	require.NoError(t, ran.err)
	assert.NotContains(t, ran.log, "level=WARN")
}

func TestExecInReadModeSaysHowToStoreTheState(t *testing.T) {
	meshStack := newFakeMeshStack(t)

	ran := execChild(t, "POST")

	require.Error(t, ran.err)
	assert.Contains(t, ran.err.Error(), "the state is read-only, run again with --mode readwrite")
	assert.NotContains(t, meshStack.requests, "POST "+statePath("my-workspace"))
}

func TestExecReadWriteRefusesWhileARunIsInProgress(t *testing.T) {
	for _, status := range []string{"IN_PROGRESS", "PENDING"} {
		t.Run(status, func(t *testing.T) {
			meshStack := newFakeMeshStack(t)
			meshStack.blockStatus = status

			refused := execChild(t, "exit=0", "--mode", "readwrite")
			require.Error(t, refused.err)
			assert.Contains(t, refused.err.Error(), "has a run "+status+", which writes the state as well")
			assert.Contains(t, refused.err.Error(), "--force")

			forced := execChild(t, "GET", "--mode", "readwrite", "--force")
			require.NoError(t, forced.err)
		})
	}
}

func TestExecTakesTheCommandAfterTheDash(t *testing.T) {
	newFakeMeshStack(t)

	ran := run(t, "exec", buildingBlockUuid, "tofu")

	require.Error(t, ran.err)
	assert.Contains(t, ran.err.Error(), "then -- and the command to run")
}
