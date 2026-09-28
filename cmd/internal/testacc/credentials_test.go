package testacc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// unsignedEmptyJwt is an unsigned JWT with an empty payload, all MESHSTACK_API_TOKEN needs to parse.
const unsignedEmptyJwt = "eyJhbGciOiJub25lIn0.e30."

// These refusals run in-process, because no invocation of the binary reaches them: `meshstack
// login` always names the credential it wants.

func TestAccCredentialResolutionRefusesTwoCredentialsAtOnce(t *testing.T) {
	newInProcessCLI(t)
	t.Setenv(setting.ApiKeyClientId.EnvKey(), "11111111-45bf-42ba-a965-2097b9d0d181")
	t.Setenv(setting.ApiKeyClientSecret.EnvKey(), "not-a-real-secret")
	t.Setenv(setting.ApiToken.EnvKey(), unsignedEmptyJwt)

	_, err := auth.ResolveClient(t.Context(), resolveOptions())
	require.ErrorContains(t, err, "resolved more than one credential")
}

func TestAccCredentialResolutionNamesEveryCredentialItLooksFor(t *testing.T) {
	newInProcessCLI(t)

	_, err := auth.ResolveClient(t.Context(), resolveOptions())
	require.ErrorContains(t, err, "selects none")
	require.ErrorContains(t, err, setting.ApiKeyClientId.EnvKey())
	require.ErrorContains(t, err, setting.ApiKeyClientSecret.EnvKey())
	require.ErrorContains(t, err, setting.ApiToken.EnvKey())
}

// newInProcessCLI is newCLI's counterpart for a resolution that runs in this process.
func newInProcessCLI(t *testing.T) {
	t.Helper()
	endpoint := requireLocalStack(t)
	t.Setenv(envConfigDir, t.TempDir())
	t.Setenv(envEndpoint, endpoint)
	t.Setenv(envProfile, "")
	t.Setenv(envWorkspace, "")
	t.Setenv(setting.ApiKeyClientId.EnvKey(), "")
	t.Setenv(setting.ApiKeyClientSecret.EnvKey(), "")
	t.Setenv(setting.ApiToken.EnvKey(), "")
}

func resolveOptions() auth.ResolveClientOptions {
	return auth.ResolveClientOptions{
		Version:    "testacc",
		GitHubRepo: "meshcloud/meshstack-cli",
	}
}
