// Package testacc runs the real `meshstack` binary against a live local meshStack, and acts as the
// browser itself where a login needs one.
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

const (
	envTestAcc = "MESHSTACK_CLI_TEST_ACC"
	// envNoBrowser is internal/oidc/browser's own escape hatch, and not a setting.Setting.
	envNoBrowser  = "MESHSTACK_CLI_NO_BROWSER"
	envEndpoint   = "MESHSTACK_ENDPOINT"
	envConfigDir  = "MESHSTACK_CONFIG_DIR"
	envProfile    = "MESHSTACK_PROFILE"
	envWorkspace  = "MESHSTACK_WORKSPACE"
	testAccOn     = "1"
	loopbackHosts = "http://localhost http://127.0.0.1"
)

var meshstack string

func TestMain(m *testing.M) {
	os.Exit(func() int {
		if os.Getenv(envTestAcc) != testAccOn {
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "meshstack-testacc")
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot create a directory for the binary under test:", err)
			return 1
		}
		defer func() { _ = os.RemoveAll(dir) }()

		// -o names the directory, so the binary keeps the name `go build ./cmd/meshstack` gives it.
		build := exec.CommandContext(context.Background(), "go", "build", "-o", dir, "./cmd/meshstack")
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

func requireLocalStack(t *testing.T) string {
	t.Helper()
	if os.Getenv(envTestAcc) != testAccOn {
		t.Skipf("acceptance tests are off. Bring up a local dev stack, export its values with `set -a; . ../.env-satellites-testacc; set +a`, and run `%s=%s go test ./cmd/internal/testacc/... -run TestAcc`",
			envTestAcc, testAccOn)
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

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	require.NotEmptyf(t, value,
		"%s is not set. `./gradlew satelliteEnv` in ../meshfed-release writes ../.env-satellites-testacc, and `set -a; . ../.env-satellites-testacc; set +a` exports it.", key)
	return value
}

// meshInfo decodes the public document into the struct the CLI decodes it into, so this suite fails
// when client.MeshInfo and the backend disagree about it.
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

type cli struct {
	t         *testing.T
	configDir string
	endpoint  string
	extraEnv  []string
}

func newCLI(t *testing.T, endpoint string) *cli {
	t.Helper()
	return &cli{t: t, configDir: t.TempDir(), endpoint: endpoint}
}

func (c *cli) setEnv(key, value string) {
	c.extraEnv = append(c.extraEnv, key+"="+value)
}

// environ blanks every MESHSTACK_* name this suite does not set on purpose, so that a developer's
// .env cannot decide what a test proves.
func (c *cli) environ() []string {
	return append(append(os.Environ(),
		envConfigDir+"="+c.configDir,
		envNoBrowser+"="+testAccOn,
		envEndpoint+"="+c.endpoint,
		envProfile+"=",
		envWorkspace+"=",
		setting.ApiKeyClientId.EnvKey()+"=",
		setting.ApiKeyClientSecret.EnvKey()+"=",
		setting.ApiToken.EnvKey()+"=",
	), c.extraEnv...)
}

func (c *cli) command(args ...string) *exec.Cmd {
	cmd := exec.CommandContext(c.t.Context(), meshstack, args...)
	cmd.Env = c.environ()
	cmd.Stdin = nil
	return cmd
}

// Every test brings its own configuration directory, so the profile is always the default one.
const defaultProfile = "default"

// The paths mirror config.Directory's layout, spelled out rather than imported: where the files
// land is what is under test.
func (c *cli) credentialsJson() string {
	return filepath.Join(c.configDir, "credentials", defaultProfile+".json")
}

func (c *cli) credentialsCacheJson(credential string) string {
	return filepath.Join(c.configDir, "credentials-cache", defaultProfile, credential+".json")
}

func (c *cli) profilesJson() string {
	return filepath.Join(c.configDir, "profiles.json")
}

// syncBuffer collects a subprocess's output while a test reads it, so the two need a lock.
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
