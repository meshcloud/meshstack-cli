package testacc

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

func TestAccApiTokenFromTheEnvironmentWinsOverTheStoredOne(t *testing.T) {
	endpoint := requireLocalStack(t)
	stored := apiKeyToken(t)
	c := newCLI(t, endpoint)
	c.setEnv(setting.ApiToken.EnvKey(), stored)
	output, err := c.run("", "login", "--apitoken")
	require.NoErrorf(t, err, "the API token login did not finish:\n%s", output)

	unsigned, _, _ := strings.CutLast(stored, ".")
	c.setEnv(setting.ApiToken.EnvKey(), unsigned+".not-the-signature")
	output, err = c.run("", "workspace", "list")
	require.Errorf(t, err, "meshStack took the stored token rather than the one in the environment:\n%s", output)
	status, err := c.run("", "auth", "status")
	require.NoErrorf(t, err, "meshstack auth status failed:\n%s", status)
	assert.Regexp(t, `\| Credential \| API token .*, from env `+setting.ApiToken.EnvKey(), status)

	c.setEnv(setting.ApiToken.EnvKey(), "")
	output, err = c.run("", "workspace", "list")
	assert.NoErrorf(t, err, "the stored token no longer works:\n%s", output)
}

func TestAccAnInputThatEndsBeforeTheProfileSelectionTakesTheCurrentProfile(t *testing.T) {
	c := newCLI(t, requireLocalStack(t)).withApiKey()
	for _, name := range []string{"other", "current"} {
		c.setEnv(envProfile, name)
		output, err := c.run("", "login", "--apikey")
		require.NoErrorf(t, err, "the API key login to profile %s did not finish:\n%s", name, output)
	}
	c.setEnv(envProfile, "")

	output, err := c.run("", "login", "--apikey")
	require.NoErrorf(t, err, "the API key login did not take the current profile:\n%s", output)
	output, err = c.run("", "auth", "logout")
	require.NoErrorf(t, err, "the logout did not take the current profile:\n%s", output)
	assert.Contains(t, output, "Logged out of profile 'current'")

	assert.NoFileExists(t, c.credentialsJsonOf("current"))
	assert.FileExists(t, c.credentialsJsonOf("other"))
}

func (c *cli) credentialsJsonOf(profile string) string {
	return filepath.Join(c.configDir, "credentials", profile+".json")
}

// apiKeyToken logs in with the API key of this suite, for a token that meshStack honours.
func apiKeyToken(t *testing.T) string {
	t.Helper()
	return cachedApiKeyToken(t, loggedInWithApiKey(t))
}
