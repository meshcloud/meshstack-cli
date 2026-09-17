// Package testacc runs the real `meshstack` binary against a live local meshStack, and where a
// login needs a browser it acts as the browser itself.
//
// It exists because nothing else can prove the browser login: a unit test can call a cobra
// command in-process, but not that the binary a user runs prints an authorization URL, waits on a
// loopback port, takes the redirect and exchanges the code. So these tests start the binary as a
// subprocess, read the URL off its stderr while it is still waiting, drive keycloak's own HTML
// forms over HTTP, and let the redirect that follows finish the login.
//
// Two things gate every test, and both are deliberate:
//
//   - MESHSTACK_TESTACC=1, without which every test skips and says how to run it. It mirrors the
//     Terraform provider's TF_ACC, so one habit covers both repositories.
//   - MESHSTACK_ENDPOINT has to name a loopback address. These tests log in and write objects,
//     and a stray export pointing them at a real meshStack is the accident worth making
//     impossible rather than merely unlikely.
//
// Every test gets its own MESHSTACK_CONFIG_DIR, and the child's environment blanks every other
// MESHSTACK_* name: the Taskfile loads a developer's .env, and a test that inherited an API key
// would not be proving anything about the login it just performed.
package testacc

import (
	"context"
	"encoding/json/v2"
	"fmt"
	gohttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// The MESHSTACK_* names this suite sets are literals, for the reason AGENTS.md gives: the CLI
// exports none of them, because every message that has to name one is produced in the package
// that consults it. The three credential settings are the exception — pkg/setting publishes them
// for a front end to read, and this suite is standing in for one.
const (
	envTestAcc = "MESHSTACK_TESTACC"
	// envNoBrowser is internal/oidc/browser's own escape hatch, and the reason this suite needs
	// no seam of its own. It is not a setting.Setting, so there is nothing to import it from.
	envNoBrowser  = "MESHSTACK_CLI_NO_BROWSER"
	envEndpoint   = "MESHSTACK_ENDPOINT"
	envConfigDir  = "MESHSTACK_CONFIG_DIR"
	envProfile    = "MESHSTACK_PROFILE"
	envWorkspace  = "MESHSTACK_WORKSPACE"
	testAccOn     = "1"
	loopbackHosts = "http://localhost http://127.0.0.1"
)

// meshstack is the binary under test, built once by TestMain.
var meshstack string

func TestMain(m *testing.M) {
	os.Exit(func() int {
		if os.Getenv(envTestAcc) != testAccOn {
			// Nothing to build: every test skips on its own, with a message saying how to run it.
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "meshstack-testacc")
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot create a directory for the binary under test:", err)
			return 1
		}
		defer func() { _ = os.RemoveAll(dir) }()

		// -o names the directory, not the binary: it keeps the name `go build ./cmd/meshstack`
		// gives it, which is the one every message and every invocation in this repository uses.
		build := exec.CommandContext(context.Background(), "go", "build", "-o", dir, "./cmd/meshstack")
		// A test runs in its own package directory, so the module root is three above.
		build.Dir = filepath.Join("..", "..", "..")
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "cannot build the meshstack binary under test:", err)
			return 1
		}
		meshstack = filepath.Join(dir, "meshstack")
		return m.Run()
	}())
}

// requireLocalStack is this suite's precheck, and every test starts with it. It answers both
// gating questions at once and returns the endpoint, so no test reads the environment itself.
func requireLocalStack(t *testing.T) string {
	t.Helper()
	if os.Getenv(envTestAcc) != testAccOn {
		t.Skipf("acceptance tests are off. Bring up a local dev stack and run `%s=%s %s=http://localhost:8080 go test ./cmd/internal/testacc/... -run TestAcc`",
			envTestAcc, testAccOn, envEndpoint)
	}
	endpoint := strings.TrimSuffix(os.Getenv(envEndpoint), "/")
	require.Truef(t, isLoopback(endpoint),
		"%s=%q does not name a loopback address. These tests log in and write objects, so they run against a local dev stack and nothing else.",
		envEndpoint, os.Getenv(envEndpoint))
	return endpoint
}

func isLoopback(endpoint string) bool {
	for host := range strings.FieldsSeq(loopbackHosts) {
		if strings.HasPrefix(endpoint, host) {
			return true
		}
	}
	return false
}

// meshInfo reads the endpoint's public document into the very struct the CLI decodes it into, so
// this suite fails when client.MeshInfo and the backend disagree about it. The issuer and the CLI
// client id come from here rather than from a constant, because a dev stack is free to move them.
func meshInfo(t *testing.T, endpoint string) client.MeshInfo {
	t.Helper()
	req, err := gohttp.NewRequestWithContext(t.Context(), gohttp.MethodGet, endpoint+"/mesh/info", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoErrorf(t, err, "%s is not answering; bring the local dev stack up first", endpoint)
	defer func() { _ = resp.Body.Close() }()
	require.Equalf(t, gohttp.StatusOK, resp.StatusCode, "%s/mesh/info answered %s", endpoint, resp.Status)

	var info client.MeshInfo
	require.NoError(t, json.UnmarshalRead(resp.Body, &info))
	require.NotEmptyf(t, info.Issuer.String(), "%s/mesh/info names no issuer, so there is no keycloak to log in at", endpoint)
	require.NotEmptyf(t, info.CliClientId, "%s/mesh/info names no cliClientId, so the CLI has no OIDC client", endpoint)
	return info
}

// cli is one test's own installation of the binary: its own configuration directory, so that no
// two tests share a profile and no test reads the developer's real one.
type cli struct {
	t         *testing.T
	configDir string
	endpoint  string
}

func newCLI(t *testing.T, endpoint string) *cli {
	t.Helper()
	return &cli{t: t, configDir: t.TempDir(), endpoint: endpoint}
}

// environ blanks every MESHSTACK_* name this suite does not set on purpose, so that a developer's
// .env cannot decide what a test proves.
func (c *cli) environ() []string {
	return append(os.Environ(),
		envConfigDir+"="+c.configDir,
		envNoBrowser+"="+testAccOn,
		envEndpoint+"="+c.endpoint,
		envProfile+"=",
		envWorkspace+"=",
		setting.ApiKeyClientId.EnvKey()+"=",
		setting.ApiKeyClientSecret.EnvKey()+"=",
		setting.ApiToken.EnvKey()+"=",
	)
}

// command builds an invocation with this installation's environment and no stdin at all, because
// nothing here is a person and the CLI must never wait for one.
func (c *cli) command(args ...string) *exec.Cmd {
	cmd := exec.CommandContext(c.t.Context(), meshstack, args...)
	cmd.Env = c.environ()
	cmd.Stdin = nil
	return cmd
}

// credentialsJson and profilesJson mirror config.Directory's layout. Spelled out rather than
// imported: a test that asked the CLI's own resolution where its files went would pass whenever
// that resolution was consistently wrong, and where the files land is what is under test.
func (c *cli) credentialsJson(profile string) string {
	return filepath.Join(c.configDir, "credentials", profile+".json")
}

func (c *cli) profilesJson() string {
	return filepath.Join(c.configDir, "profiles.json")
}

// syncBuffer collects a subprocess's output while a test reads it, from the goroutine that scans
// stderr. The two need a lock between them.
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
