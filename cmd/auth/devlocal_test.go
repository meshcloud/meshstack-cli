package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/cli"
	"github.com/meshcloud/meshstack-cli/pkg/auth"
)

const (
	devApiKeyClientId     = "37abbe45-aba7-4617-b87d-93f4cbf95832"
	devApiKeyClientSecret = "eUp1jPMfM2RyNOjdVRuLmHGOYCvzZrN5"
	devAdminWorkspace     = "demo-partner"
)

// --dev-local bootstraps a profile out of a document nobody configured, so the assertions are
// about what reached disk: the profile in config.json, and the credential the CLI can use next.
func TestDevLocalLogin(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantProfile string
	}{
		{
			name:        "the reserved profile name is the default",
			args:        nil,
			wantProfile: auth.DevLocalProfile,
		},
		{
			name:        "--profile overrides the reserved name",
			args:        []string{"--profile", "scratch"},
			wantProfile: "scratch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := isolateAt(t)
			stack := devLocalStack(t, true)

			cmd := loginWithRootFlags(cli.New())
			require.NoError(t, run(cmd, append([]string{"--dev-local", "--endpoint", stack}, test.args...)...))

			config, err := os.ReadFile(filepath.Join(dir, "config.json"))
			require.NoError(t, err)
			assert.Contains(t, string(config), test.wantProfile)
			assert.Contains(t, string(config), stack)
			// An ADM_ key is not workspace-bound, but a profile with no workspace fails every
			// workspace-scoped command later, so the admin workspace becomes the default.
			assert.Contains(t, string(config), devAdminWorkspace)

			credentials, err := os.ReadFile(filepath.Join(dir, "credentials", test.wantProfile+".json"))
			require.NoError(t, err)
			assert.Contains(t, string(credentials), devApiKeyClientId)
			assert.Contains(t, string(credentials), devApiKeyClientSecret)
			assert.Contains(t, string(credentials), `"current": "apiKey"`)
		})
	}
}

// Every meshStack a user could reach answers without the field, so this is the message most
// people who try the flag will ever see.
func TestDevLocalLoginAgainstAnOrdinaryMeshStack(t *testing.T) {
	isolateAt(t)
	stack := devLocalStack(t, false)

	err := run(loginWithRootFlags(cli.New()), "--dev-local", "--endpoint", stack)

	require.Error(t, err)
	assert.Contains(t, err.Error(), stack, "the message names the endpoint that was asked")
	assert.Contains(t, err.Error(), "local dev stack")
	assert.Contains(t, err.Error(), "--api-key", "and points at the alternative")
}

func TestDevLocalAndApiKeyAreMutuallyExclusive(t *testing.T) {
	isolateAt(t)

	err := run(NewLogin(cli.New()), "--dev-local", "--api-key")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dev-local")
	assert.Contains(t, err.Error(), "api-key")
	assert.Contains(t, err.Error(), "none of the others can be")
}

// loginWithRootFlags gives `login` the two persistent flags cmd/meshstack binds on the root
// command, so a test can drive it on its own and still say --endpoint and --profile.
func loginWithRootFlags(in *cli.Input) *cobra.Command {
	cmd := NewLogin(in)
	cmd.Flags().StringVar(&in.Endpoint, "endpoint", "", "meshStack API endpoint")
	cmd.Flags().StringVar(&in.Profile, "profile", "", "configuration profile to use")
	return cmd
}

// isolateAt points the CLI at an empty configuration directory and clears every MESHSTACK_*
// variable, so that no test reads a developer's real profile — the Taskfile loads .env into
// `task test`, so these are often set. It returns the directory, for a test that asserts on
// the files a command wrote.
func isolateAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MESHSTACK_CONFIG_DIR", dir)
	for _, key := range []string{
		"MESHSTACK_ENDPOINT", "MESHSTACK_WORKSPACE", "MESHSTACK_PROFILE",
		"MESHSTACK_API_KEY", "MESHSTACK_API_SECRET", "MESHSTACK_API_TOKEN",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("MESHSTACK_NO_INPUT", "1")
	return dir
}

// devLocalStack serves the two endpoints --dev-local touches: the public document it reads its
// credentials out of, and the exchange that turns them into a token.
func devLocalStack(t *testing.T, withDevLocalCredentials bool) string {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/mesh/info", func(w http.ResponseWriter, _ *http.Request) {
		info := map[string]any{
			"version":                  "2026.34.0",
			"issuer":                   server.URL + "/auth/realms/meshfed",
			"cliClientId":              "meshstack-cli",
			"adminWorkspaceIdentifier": devAdminWorkspace,
		}
		if withDevLocalCredentials {
			info["devLocalCredentials"] = map[string]any{
				"apiKeyClientId":     devApiKeyClientId,
				"apiKeyClientSecret": devApiKeyClientSecret,
				"users": []map[string]any{
					{"username": "partner@meshcloud.io", "password": "sample123", "workspace": devAdminWorkspace},
				},
			}
		}
		writeJSON(w, info)
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"access_token": devLocalToken(), "expires_in": 300})
	})
	return server.URL
}

func writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// devLocalToken is what /api/login answers with. Nothing verifies a signature, but the deadline
// has to be real: pkg/auth reads it out of the exp claim to decide the token is usable.
func devLocalToken() string {
	claims, _ := json.Marshal(map[string]any{
		"jti": "dev-local-token",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})
	return "x." + base64.RawURLEncoding.EncodeToString(claims) + ".y"
}
