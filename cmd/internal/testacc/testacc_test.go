package testacc

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/api"
	"github.com/meshcloud/meshstack-cli/cmd/auth"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockdefinition"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockdefinitionversion"
	"github.com/meshcloud/meshstack-cli/cmd/buildingblockrun"
	"github.com/meshcloud/meshstack-cli/cmd/eventlog"
	"github.com/meshcloud/meshstack-cli/cmd/profile"
	"github.com/meshcloud/meshstack-cli/cmd/workspace"
	"github.com/meshcloud/meshstack-cli/pkg/io"
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

// environ blanks every MESHSTACK_* name this suite does not set on purpose, and the names that
// force color, so that a developer's .env or a CI cannot decide what a test proves.
func (c *cli) environ() []string {
	return append([]string{
		envConfigDir + "=" + c.configDir,
		envNoBrowser + "=" + testAccOn,
		envEndpoint + "=" + c.endpoint,
		envProfile + "=",
		envWorkspace + "=",
		setting.SkipVersionCheck.EnvKey() + "=",
		setting.ApiKeyClientId.EnvKey() + "=",
		setting.ApiKeyClientSecret.EnvKey() + "=",
		setting.ApiToken.EnvKey() + "=",
		"FORCE_COLOR=",
		"CLICOLOR_FORCE=",
		"TTY_FORCE=",
	}, c.extraEnv...)
}

// newRootCommand holds the commands this suite runs. The binary's own root is in package main, which
// no test can import, and adds only persistent flags, which this suite sets through the environment.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "meshstack", SilenceUsage: true}
	root.AddCommand(api.New(), auth.New(), auth.NewLogin(),
		buildingblock.New(), buildingblockdefinition.New(), buildingblockdefinitionversion.New(),
		buildingblockrun.New(), eventlog.New(), profile.New(), workspace.New())
	return root
}

// cliRun is a command that runs in process, in the environment of its cli. Its output holds what the
// binary would write to stdout and stderr, and the log, in the order it was written.
type cliRun struct {
	output   *syncBuffer
	cancel   context.CancelFunc
	finished chan struct{}
	err      error
}

func (c *cli) applyEnv() {
	for _, variable := range c.environ() {
		key, value, _ := strings.Cut(variable, "=")
		c.t.Setenv(key, value)
	}
}

// start sets the environment with t.Setenv, so no two commands of a test can run at the same time.
func (c *cli) start(stdin string, args ...string) *cliRun {
	c.t.Helper()
	c.applyEnv()
	ctx, cancel := context.WithCancel(c.t.Context())
	run := &cliRun{output: &syncBuffer{}, cancel: cancel, finished: make(chan struct{})}
	previous := slog.Default()
	c.t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(slog.New(slog.NewTextHandler(run.output, nil)))

	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(run.output)
	cmd.SetErr(run.output)
	go func() {
		defer close(run.finished)
		run.err = cmd.ExecuteContext(io.WithStderr(ctx, run.output))
	}()
	return run
}

func (r *cliRun) wait() error {
	<-r.finished
	r.cancel()
	return r.err
}

// abort ends a login that will never finish, because the redirect it waits for never comes.
func (r *cliRun) abort() {
	r.cancel()
	<-r.finished
}

func (c *cli) run(stdin string, args ...string) (string, error) {
	c.t.Helper()
	run := c.start(stdin, args...)
	err := run.wait()
	return run.output.String(), err
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
