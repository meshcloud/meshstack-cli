package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

// execute runs a command line of meshstack auth, or of the login shortcut, with no input.
func execute(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "meshstack", SilenceUsage: true, SilenceErrors: true}
	for _, flag := range []*internal.Flag[string]{&internal.ProfileFlag, &internal.EndpointFlag, &internal.WorkspaceFlag} {
		flag.Value = ""
		flag.Register(root.PersistentFlags())
	}
	root.AddCommand(New(), NewLogin())
	var out bytes.Buffer
	root.SetIn(&bytes.Buffer{})
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	err = root.ExecuteContext(t.Context())
	return out.String(), err
}

func executeStatus(t *testing.T, args ...string) string {
	t.Helper()
	stdout, err := execute(t, append([]string{"auth", "status"}, args...)...)
	require.NoError(t, err)
	return stdout
}

func withApiToken(t *testing.T) {
	t.Helper()
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		assert.Equal(t, "/mesh/info", r.URL.Path)
		_, _ = w.Write([]byte(`{"version": "2026.40.0"}`))
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)
	claims := fmt.Appendf(nil, `{"exp":%d,"preferred_username":"runner","MC_CUSTOMER":"ops"}`, time.Now().Add(time.Hour).Unix())
	t.Setenv(auth.ApiTokenSetting.EnvKey(), "e30."+base64.RawURLEncoding.EncodeToString(claims)+".test-signature")
}

func TestStatusOfAnApiTokenFromTheEnvironment(t *testing.T) {
	withApiToken(t)

	t.Run("is Markdown where the output is no terminal", func(t *testing.T) {
		output := executeStatus(t)

		assert.Contains(t, output, "| Profile | default |\n| --- | --- |\n")
		assert.Contains(t, output, "| Credential | API token, from env MESHSTACK_API_TOKEN |\n")
		assert.Contains(t, output, "|  | User runner |\n")
		assert.Contains(t, output, "| meshStack | 2026.40.0 |\n")
		assert.NotContains(t, output, "\x1b[", "no terminal styling")
	})

	t.Run("is JSON for scripts", func(t *testing.T) {
		var status struct {
			Credential       string `json:"credential"`
			MeshStackVersion string `json:"meshStackVersion"`
			Token            struct {
				User      string `json:"user"`
				Workspace string `json:"workspace"`
			} `json:"token"`
		}
		require.NoError(t, json.Unmarshal([]byte(executeStatus(t, "-o", "json")), &status))

		assert.Equal(t, "manual", status.Credential)
		assert.Equal(t, "2026.40.0", status.MeshStackVersion)
		assert.Equal(t, "runner", status.Token.User)
		assert.Equal(t, "ops", status.Token.Workspace)
	})
}
