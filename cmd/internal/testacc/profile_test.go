package testacc

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const profilesInUse = "another meshstack command is changing the profiles"

func TestAccProfiles(t *testing.T) {
	c := newCLI(t, requireLocalStack(t)).withApiKey()

	t.Run("a login fails while meshstack profile holds the profiles", func(t *testing.T) {
		c.applyEnv()
		// meshstack profile cannot open in a test, so the test holds the profiles in its place.
		release := testlogin.HoldProfiles(t)

		output, err := c.run("1\n", "login", "--apikey")
		require.Errorf(t, err, "the login ran while meshstack profile held the profiles:\n%s", output)
		assert.Contains(t, err.Error(), "cannot log in while "+profilesInUse+", such as meshstack profile")
		assert.NoFileExists(t, c.credentialsJson())

		release()
		output, err = c.run("1\n", "login", "--apikey")
		require.NoErrorf(t, err, "the login failed once the profiles were free:\n%s", output)
		requireStoredLogin(t, c, output)
	})

	t.Run("an input that ends before the profile selection takes the current profile", func(t *testing.T) {
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
		assert.FileExists(t, c.credentialsJson())
	})
}

func browserLoginHoldsTheProfiles(endpoint string, login devLogin) func(*testing.T) {
	return func(t *testing.T) {
		c := newCLI(t, endpoint)
		run := startLogin(t, c, "1")
		startURL := run.awaitStartURL(t)

		added, err := c.run("", "profile", "add")
		require.Errorf(t, err, "meshstack profile add ran while the login waited for the browser:\n%s", added)
		assert.Contains(t, err.Error(), profilesInUse+", such as a login")
		listed, err := c.run("", "profile", "list")
		require.NoErrorf(t, err, "meshstack profile list only reads, and runs alongside a login:\n%s", listed)

		completeKeycloakLogin(t, startURL, "full", login.Username, login.Password)
		require.NoErrorf(t, run.wait(), "the browser login did not finish:\n%s", run.output.String())
		shown, err := c.run("", "profile", "show")
		require.NoErrorf(t, err, "meshstack profile show failed:\n%s", shown)
		assert.Contains(t, shown, "| Credential | Browser login, from file ", "show reads the status of the stored login")
	}
}

func (c *cli) credentialsJsonOf(profile string) string {
	return filepath.Join(c.configDir, "credentials", profile+".json")
}
