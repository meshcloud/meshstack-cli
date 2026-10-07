package tfstate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
	state             = `{"version":4,"serial":1,"lineage":"lineage-1","resources":[]}`
	childSteps        = "MESHSTACK_TFSTATE_TEST_CHILD"
	runUuid           = "a1d2c3e4-0000-4000-8000-000000000002"
)

var apiKey = fakemeshstack.ApiKey{ClientId: "11111111-45bf-42ba-a965-2097b9d0d181", ClientSecret: "the-secret"}

func statePath(workspace string) string {
	return "/api/terraform/state/workspace/" + workspace + "/buildingBlock/" + buildingBlockUuid
}

// TestMain runs the steps of childSteps where exec started the test binary: GET asks the proxy for
// the state, POST stores state, provider reads the building block as tofu's meshstack provider
// does, and exit=<code> exits.
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
		if step == "provider" {
			if err := readAsTheProvider(); err != nil {
				fmt.Println(err)
				return 1
			}
			continue
		}
		address, _ := url.Parse(os.Getenv("TF_HTTP_ADDRESS"))
		r := httptest.NewRequestWithContext(context.Background(), step, "/", nil)
		r.SetBasicAuth(os.Getenv("TF_HTTP_USERNAME"), os.Getenv("TF_HTTP_PASSWORD"))
		_, _ = http.NewClient(fakemeshstack.UserAgent).DoRequest[[]byte](context.Background(), step, address, http.WithHeaders(r.Header), http.WithBody([]byte(state)))
	}
	return 0
}

func readAsTheProvider() error {
	ctx := context.Background()
	meshStack, err := auth.ResolveClient(ctx, auth.ResolveClientOptions{Version: "test", GitHubRepo: "meshcloud/terraform-provider-meshstack"})
	if err != nil {
		return err
	}
	block, err := meshStack.Raw.DoRequest(ctx, http.MethodGet, "/api/meshobjects/meshbuildingblocks/"+buildingBlockUuid)
	fmt.Print(string(block))
	return err
}

type fakeMeshStack struct {
	*fakemeshstack.Server

	blockStatus map[string]any
	runStatus   map[string]any
}

func newFakeMeshStack(t *testing.T) *fakeMeshStack {
	t.Helper()
	blockStatus := map[string]any{"status": "SUCCEEDED"}
	run := map[string]any{"metadata": map[string]any{"uuid": runUuid}, "status": "IN_PROGRESS"}
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		ApiKeys:           []fakemeshstack.ApiKey{apiKey},
		BuildingBlockRuns: []any{run},
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
	return &fakeMeshStack{server, blockStatus, run}
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
	return runAnswering(t, "", args...)
}

func runAnswering(t *testing.T, answers string, args ...string) result {
	t.Helper()
	var stdout, stderr, log bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	cmd := tfstate.New()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(answers))
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

	t.Run("exec lets the meshstack provider read meshStack with the login of the CLI", func(t *testing.T) {
		t.Setenv(setting.ApiToken.EnvKey(), "")
		t.Setenv(setting.ApiKeyClientId.EnvKey(), apiKey.ClientId)
		t.Setenv(setting.ApiKeyClientSecret.EnvKey(), apiKey.ClientSecret)
		logins := meshStack.Counts().Logins

		ran := execChild(t, "GET provider")

		require.NoError(t, ran.err, ran.stdout)
		assert.Contains(t, ran.stdout, `"ownedByWorkspace":"my-workspace"`)
		assert.Equal(t, logins+1, meshStack.Counts().Logins, "only exec logs in with the API key")
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

	t.Run("exec succeeds where meshStack failed a request that the command then got past", func(t *testing.T) {
		meshStack.Route("/api/terraform/state/", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusInternalServerError)
		})
		defer meshStack.Route("/api/terraform/state/", nil)

		ran := execChild(t, "GET exit=0")

		require.NoError(t, ran.err)
		assert.Contains(t, ran.log, "level=WARN")
		assert.Contains(t, ran.log, "http error 500")
	})

	t.Run("a refusal of meshStack says which rights the state takes", func(t *testing.T) {
		meshStack.Route("/api/terraform/state/", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusForbidden)
		})

		err := execChild(t, "GET").err

		require.Error(t, err)
		assert.Contains(t, err.Error(), "http error 403")
		assert.Contains(t, err.Error(), "MANAGED_TFSTATE_LIST")
		assert.Contains(t, err.Error(), "meshstack login --apikey")
	})
}

func TestForceUnlock(t *testing.T) {
	meshStack := newFakeMeshStack(t)
	lockPath := statePath("my-workspace") + "/lock"
	var lock []byte
	meshStack.Route(lockPath, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		switch {
		case r.Method == gohttp.MethodGet && lock == nil:
			w.WriteHeader(gohttp.StatusNotFound)
		case r.Method == gohttp.MethodGet:
			_, _ = w.Write(lock)
		case r.Method == gohttp.MethodDelete:
			lock = nil
		}
	})
	lockOfTheRun := []byte(`{"lockInfo":{"ID":"the-lock","Operation":"OperationTypeApply","Who":"building block run ` + runUuid + `"},` +
		`"holder":{"runUuid":"` + runUuid + `","principal":"the runner"},"createdOn":"2026-10-05T09:00:00Z"}`)

	const question = "Release the lock the-lock? Only yes releases it: "

	t.Run("refuses to release the lock of a run in progress, before it asks", func(t *testing.T) {
		lock = lockOfTheRun

		refused := runAnswering(t, "yes\n", "force-unlock", buildingBlockUuid)

		require.Error(t, refused.err)
		assert.Contains(t, refused.err.Error(), "building block run "+runUuid+", which holds the lock, is IN_PROGRESS")
		assert.Contains(t, refused.log, "is locked by building block run "+runUuid)
		assert.NotContains(t, refused.stderr, question)
		assert.NotNil(t, lock)
	})

	t.Run("--force still asks, and any answer but yes keeps the lock", func(t *testing.T) {
		for _, answers := range []string{"y\n", "YES\n", ""} {
			kept := runAnswering(t, answers, "force-unlock", buildingBlockUuid, "--force")

			require.Error(t, kept.err)
			assert.Contains(t, kept.err.Error(), "kept the lock the-lock")
			assert.Contains(t, kept.stderr, question)
			assert.NotNil(t, lock)
		}
	})

	t.Run("releases the lock of a run in progress with --force and yes, by the lock ID it found", func(t *testing.T) {
		meshStack.TakeRequests()

		released := runAnswering(t, "yes\n", "force-unlock", buildingBlockUuid, "--force")

		require.NoError(t, released.err)
		assert.Nil(t, lock)
		requests := meshStack.TakeRequests()
		unlock := requests[len(requests)-1]
		assert.Equal(t, gohttp.MethodDelete, unlock.Method)
		assert.Contains(t, string(unlock.Body), `"ID":"the-lock"`)
	})

	t.Run("releases the lock of a finished run on yes", func(t *testing.T) {
		meshStack.runStatus["status"] = "FAILED"
		lock = lockOfTheRun

		require.NoError(t, runAnswering(t, "yes\n", "force-unlock", buildingBlockUuid).err)
		assert.Nil(t, lock)
	})

	t.Run("succeeds where the state has no lock, without asking", func(t *testing.T) {
		released := run(t, "force-unlock", buildingBlockUuid)

		require.NoError(t, released.err)
		assert.Contains(t, released.log, "has no lock")
		assert.Empty(t, released.stderr)
	})
}
