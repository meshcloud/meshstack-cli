package testacc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/auth"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

// The credential resolution's three refusals run in-process, because no invocation of the binary
// reaches them: `meshstack login` always names the credential it wants.
//
// An unsigned JWT with an empty payload, which is all MESHSTACK_API_TOKEN needs to parse.
const unsignedEmptyJwt = "eyJhbGciOiJub25lIn0.e30."

func TestAccCredentialResolutionRefusesAnUnknownForcedCredential(t *testing.T) {
	newInProcessCLI(t)

	_, err := auth.ResolveSession(t.Context(), resolveOptions(auth.Method("nope")))
	require.ErrorContains(t, err, "cannot authenticate with credential 'nope'; pick one of [apiKey manual oidcLogin]")
}

func TestAccCredentialResolutionRefusesTwoCredentialsAtOnce(t *testing.T) {
	newInProcessCLI(t)
	t.Setenv(setting.ApiKeyClientId.EnvKey(), "11111111-45bf-42ba-a965-2097b9d0d181")
	t.Setenv(setting.ApiKeyClientSecret.EnvKey(), "not-a-real-secret")
	t.Setenv(setting.ApiToken.EnvKey(), unsignedEmptyJwt)

	_, err := auth.ResolveSession(t.Context(), resolveOptions(""))
	require.ErrorContains(t, err, "resolved more than one credential")
}

func TestAccCredentialResolutionNamesEveryCredentialItLooksFor(t *testing.T) {
	newInProcessCLI(t)

	_, err := auth.ResolveSession(t.Context(), resolveOptions(""))
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

func resolveOptions(forced auth.Method) auth.ResolveSessionOptions {
	return auth.ResolveSessionOptions{
		Version:       "testacc",
		GitHubRepo:    "meshcloud/meshstack-cli",
		ForceAuthWith: forced,
	}
}
