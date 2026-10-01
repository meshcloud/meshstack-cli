package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

func executeStatus(t *testing.T, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "meshstack", SilenceUsage: true, SilenceErrors: true}
	for _, flag := range []*internal.Flag[string]{&internal.ProfileFlag, &internal.EndpointFlag, &internal.WorkspaceFlag} {
		flag.Value = ""
		flag.Register(root.PersistentFlags())
	}
	root.AddCommand(New())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(append([]string{"auth", "status"}, args...))
	require.NoError(t, root.ExecuteContext(t.Context()))
	return out.String()
}

func withApiToken(t *testing.T) {
	t.Helper()
	for _, envKey := range []string{
		profile.NameSetting.EnvKey(), meshstack.WorkspaceSetting.EnvKey(),
		auth.ApiKeyClientIdSetting.EnvKey(), auth.ApiKeyClientSecretSetting.EnvKey(),
	} {
		t.Setenv(envKey, "")
	}
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), "https://meshstack.example.com")
	claims := fmt.Appendf(nil, `{"exp":%d,"preferred_username":"runner","MC_CUSTOMER":"ops"}`, time.Now().Add(time.Hour).Unix())
	t.Setenv(auth.ApiTokenSetting.EnvKey(), "e30."+base64.RawURLEncoding.EncodeToString(claims)+".test-signature")
}

func TestStatusShowsTheCredentialAsMarkdownWhereTheOutputIsNoTerminal(t *testing.T) {
	withApiToken(t)

	output := executeStatus(t)

	assert.Contains(t, output, "| Profile | default |\n| --- | --- |\n")
	assert.Contains(t, output, "| Credential | API token, from env MESHSTACK_API_TOKEN |\n")
	assert.Contains(t, output, "|  | User runner |\n")
	assert.NotContains(t, output, "\x1b[", "no terminal styling")
}

func TestStatusWritesJsonForScripts(t *testing.T) {
	withApiToken(t)

	var status struct {
		Credential string `json:"credential"`
		Token      struct {
			User      string `json:"user"`
			Workspace string `json:"workspace"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal([]byte(executeStatus(t, "-o", "json")), &status))

	assert.Equal(t, "manual", status.Credential)
	assert.Equal(t, "runner", status.Token.User)
	assert.Equal(t, "ops", status.Token.Workspace)
}
