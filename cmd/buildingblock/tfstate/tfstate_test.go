package tfstate_test

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock/tfstate"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
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
	*fakemeshstack.Server

	blockStatus map[string]any
}

func newFakeMeshStack(t *testing.T) *fakeMeshStack {
	t.Helper()
	blockStatus := map[string]any{"status": "SUCCEEDED"}
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{map[string]any{
			"metadata": map[string]any{"uuid": buildingBlockUuid, "ownedByWorkspace": "my-workspace"},
			"spec":     map[string]any{},
			"status":   blockStatus,
		}},
		TfStates: map[fakemeshstack.TfState][]byte{
			{Workspace: "my-workspace", BuildingBlockUuid: buildingBlockUuid}:    []byte(state),
			{Workspace: "other-workspace", BuildingBlockUuid: buildingBlockUuid}: []byte(state),
		},
	})
	testlogin.LoggedInTo(t, server.URL)
	return &fakeMeshStack{server, blockStatus}
}

func (f *fakeMeshStack) requests() (requests []string) {
	for _, request := range f.TakeRequests() {
		requests = append(requests, request.Method+" "+request.URL.Path)
	}
	return requests
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
	cmd := tfstate.New()
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

func TestTfstate(t *testing.T) {
	meshStack := newFakeMeshStack(t)

	t.Run("show takes the state of the workspace given, rather than the building block's", func(t *testing.T) {
		t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "other-workspace")
		meshStack.TakeRequests()

		shown := run(t, "show", buildingBlockUuid)

		require.NoError(t, shown.err)
		assert.Equal(t, state, shown.stdout, "the state as it came, not merely the same JSON") //nolint:testifylint // encoded-compare: JSONEq would take the state reformatted
		assert.Equal(t, []string{"GET " + statePath("other-workspace")}, meshStack.requests())
	})

	t.Run("exec exits with the exit code of the command", func(t *testing.T) {
		ran := execChild(t, "GET exit=3")

		exitErr, ok := errors.AsType[internal.ExitError](ran.err)
		require.True(t, ok, "%v", ran.err)
		assert.Equal(t, 3, exitErr.Code)
		assert.Empty(t, ran.stderr, "the command has said why it failed")
		assert.NotContains(t, ran.log, "level=WARN", "the command asked for the state")
	})

	t.Run("exec warns where the command asked for no state", func(t *testing.T) {
		ran := execChild(t, "exit=0")

		require.NoError(t, ran.err)
		assert.Contains(t, ran.log, `asked for no state, so the module likely has no backend \"http\" block`)
		assert.Contains(t, ran.log, "meshstack_backend.tf")
	})

	t.Run("exec in read mode says how to store the state", func(t *testing.T) {
		meshStack.TakeRequests()

		ran := execChild(t, "POST")

		require.Error(t, ran.err)
		assert.Contains(t, ran.err.Error(), "the state is read-only, run again with --mode readwrite")
		assert.NotContains(t, meshStack.requests(), "POST "+statePath("my-workspace"))
	})

	t.Run("exec in readwrite mode refuses while a run is in progress, unless forced", func(t *testing.T) {
		defer func() { meshStack.blockStatus["status"] = "SUCCEEDED" }()
		for _, status := range []string{"IN_PROGRESS", "PENDING"} {
			meshStack.blockStatus["status"] = status

			refused := execChild(t, "exit=0", "--mode", "readwrite")
			require.Error(t, refused.err, status)
			assert.Contains(t, refused.err.Error(), "has a run "+status+", which writes the state as well")
			assert.Contains(t, refused.err.Error(), "--force")

			forced := execChild(t, "GET", "--mode", "readwrite", "--force")
			require.NoError(t, forced.err, status)
		}
	})

	t.Run("a refusal of meshStack says that the state takes an API key", func(t *testing.T) {
		meshStack.Route("/api/terraform/state/", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusForbidden)
		})

		err := execChild(t, "GET").err

		require.Error(t, err)
		assert.Contains(t, err.Error(), "http error 403")
		assert.Contains(t, err.Error(), "This command needs an API key login, meshstack login --apikey")
		assert.Contains(t, err.Error(), "TFSTATE_LIST")
	})
}
