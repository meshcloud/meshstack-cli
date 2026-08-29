// Package acceptance drives the built `meshstack` binary against a live local meshStack.
//
// It is a package of tests and nothing else, because that is what it is testing: everything
// under cmd/ has unit tests that call a cobra command in-process, and none of those can prove
// that the binary a user runs logs in, writes files a later invocation reads back, and exits
// with the right status. So these run it as a subprocess, exactly as a person would.
//
// Two things gate them, and both are deliberate:
//
//   - MESHSTACK_ACC=1, without which every test skips and says how to run it. It mirrors the
//     Terraform provider's TF_ACC, so one habit covers both repositories.
//   - MESHSTACK_ENDPOINT has to name a loopback address. These tests log in and write objects,
//     and a stray export pointing them at a real meshStack is the accident worth making
//     impossible rather than merely unlikely.
//
// Each test gets its own MESHSTACK_CONFIG_FILE and MESHSTACK_CREDENTIALS_DIR under a temporary
// directory, and the child's environment blanks every other MESHSTACK_* name — the Taskfile
// loads a developer's .env, and a test that inherited an endpoint or an API key would not be
// proving that the profile it just wrote is what the next command uses.
package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
)

// The MESHSTACK_* names are literals here for the reason AGENTS.md gives: the CLI exports
// none of them, because every message that has to name one is produced in the package that
// consults it. So this suite keeps its own copies of the ones it sets.
const (
	envAcc               = "MESHSTACK_ACC"
	envEndpoint          = "MESHSTACK_ENDPOINT"
	envConfigFile        = "MESHSTACK_CONFIG_FILE"
	envCredentialsDir    = "MESHSTACK_CREDENTIALS_DIR"
	envSkipVersionCheck  = "MESHSTACK_SKIP_VERSION_CHECK"
	envProfile           = "MESHSTACK_PROFILE"
	envWorkspace         = "MESHSTACK_WORKSPACE"
	envApiKey            = "MESHSTACK_API_KEY"
	envApiSecret         = "MESHSTACK_API_SECRET"
	envApiToken          = "MESHSTACK_API_TOKEN"
	loopbackHost         = "http://localhost"
	loopbackAddressHost  = "http://127.0.0.1"
	acceptanceEnabled    = "1"
	skipVersionCheckHint = "true"
)

// reachabilityTimeout bounds the precheck's one request: generous for a loopback GET of a public
// document, short enough that a stack which is down is reported rather than waited on.
const reachabilityTimeout = 10 * time.Second

// meshstack is the binary under test, built once by TestMain.
var meshstack string

func TestMain(m *testing.M) {
	os.Exit(func() int {
		if os.Getenv(envAcc) != acceptanceEnabled {
			// Nothing to build: every test skips on its own, with a message saying how to run it.
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "meshstack-acceptance")
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot create a directory for the binary under test:", err)
			return 1
		}
		defer func() { _ = os.RemoveAll(dir) }()

		// -o names the directory, not the binary: it keeps the name `go build ./cmd/meshstack`
		// gives it, which is the one every message and every invocation in this repository uses.
		build := exec.Command("go", "build", "-o", filepath.Join(dir, "meshstack"), "./cmd/meshstack")
		// A test runs in its own package directory, so the module root is one above.
		build.Dir = ".."
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "cannot build the meshstack binary under test:", err)
			return 1
		}
		meshstack = filepath.Join(dir, "meshstack")
		return m.Run()
	}())
}

// requireLocalStack is this suite's precheck, and every test starts with it. It answers all
// three gating questions at once — is the suite on, is the endpoint loopback, is the backend
// there — and returns the endpoint with what /mesh/info said, so no test reads the environment
// itself and none has to run before another to establish that the stack is up.
func requireLocalStack(t *testing.T) (string, client.MeshInfo) {
	t.Helper()
	if os.Getenv(envAcc) != acceptanceEnabled {
		t.Skipf("acceptance tests are off. Bring up a local dev stack and run `%s=%s %s=%s:8080 go test ./acceptance/... -run TestAcc`",
			envAcc, acceptanceEnabled, envEndpoint, loopbackHost)
	}
	endpoint := strings.TrimSuffix(os.Getenv(envEndpoint), "/")
	require.Truef(t,
		strings.HasPrefix(endpoint, loopbackHost) || strings.HasPrefix(endpoint, loopbackAddressHost),
		"%s=%q does not name a loopback address. These tests log in and write objects, so they run against a local dev stack and nothing else.",
		envEndpoint, os.Getenv(envEndpoint))
	return endpoint, meshInfo(t, endpoint)
}

// meshstackCLI is one test's own installation: its own configuration file and its own
// credentials directory, so that two tests never share a profile.
type meshstackCLI struct {
	t           *testing.T
	config      string
	credentials string
}

func newCLI(t *testing.T) *meshstackCLI {
	t.Helper()
	dir := t.TempDir()
	return &meshstackCLI{
		t:           t,
		config:      filepath.Join(dir, "config.yaml"),
		credentials: filepath.Join(dir, "credentials"),
	}
}

func (c *meshstackCLI) environ() []string {
	return append(os.Environ(),
		envConfigFile+"="+c.config,
		envCredentialsDir+"="+c.credentials,
		// The dev stack reports a version below the client's minimum, which is a statement
		// about the backend rather than about anything under test here.
		envSkipVersionCheck+"="+skipVersionCheckHint,
		// MESHSTACK_NO_INPUT is deliberately not set. It means "nobody is coming", and the
		// browser login refuses outright rather than waiting when it is — which would defeat
		// TestAccBrowserLoginHeadless. Nothing here prompts anyway: a prompt needs a terminal
		// on stdin, and these subprocesses are given none.
		//
		// Blanked rather than inherited: see the package comment.
		envEndpoint+"=",
		envProfile+"=",
		envWorkspace+"=",
		envApiKey+"=",
		envApiSecret+"=",
		envApiToken+"=",
	)
}

// command builds an invocation with this installation's environment and no stdin at all,
// because nothing here is a person and the CLI must never wait for one.
func (c *meshstackCLI) command(args ...string) *exec.Cmd {
	cmd := exec.CommandContext(c.t.Context(), meshstack, args...)
	cmd.Env = c.environ()
	cmd.Stdin = nil
	return cmd
}

func (c *meshstackCLI) run(args ...string) (string, error) {
	c.t.Helper()
	output, err := c.command(args...).CombinedOutput()
	c.t.Logf("$ meshstack %s\n%s", strings.Join(args, " "), output)
	return string(output), err
}

func (c *meshstackCLI) mustRun(args ...string) string {
	c.t.Helper()
	output, err := c.run(args...)
	require.NoErrorf(c.t, err, "`meshstack %s` failed:\n%s", strings.Join(args, " "), output)
	return output
}

// meshInfo reads the endpoint's public document into the very struct the CLI decodes it into,
// so this suite fails when client.MeshInfo and the backend disagree about it. It is also how the
// precheck learns the backend is up, which is worth a request of its own: a login discovers that
// only after the minute internal/http spends retrying, and reports it as a timeout rather than
// as a stack that is down.
func meshInfo(t *testing.T, endpoint string) client.MeshInfo {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/mesh/info", nil)
	require.NoError(t, err)

	// Its own client, because the timeout belongs to this request alone: an address that accepts
	// and never answers has to fail here in seconds, while the browser login waits minutes by
	// design and http.DefaultClient is shared with whatever else ends up using it.
	resp, err := (&http.Client{Timeout: reachabilityTimeout}).Do(req)
	require.NoErrorf(t, err, "the backend at %s is not reachable, so nothing in this package can run. Bring the local dev stack up first.", endpoint)
	defer func() { _ = resp.Body.Close() }()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "%s/mesh/info answered %s", endpoint, resp.Status)

	var info client.MeshInfo
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&info))
	return info
}

// requireDevLocalCredentials skips rather than fails while the field is still landing: the
// meshfed:latest image CI runs will not carry devLocalCredentials until meshfed's change that
// serves it from /mesh/info merges.
func requireDevLocalCredentials(t *testing.T, endpoint string, info client.MeshInfo) *client.DevLocalCredentials {
	t.Helper()
	if info.DevLocalCredentials == nil {
		t.Skipf("%s serves no devLocalCredentials in /mesh/info, so there is nothing to bootstrap from. This needs the meshfed change that publishes the local dev stack's own credentials there.", endpoint)
	}
	return info.DevLocalCredentials
}

// syncBuffer collects a subprocess's output while a test reads it. os/exec writes from its own
// goroutine, so the two need a lock between them.
type syncBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
