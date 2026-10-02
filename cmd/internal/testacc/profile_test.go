package testacc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

// The commands that change the profiles hold them for as long as a person takes, so these tests
// check that a browser login holds them until the browser comes back, and that a login waits for
// nobody: meshstack profile cannot open in a test, so the test holds the lock as it does.
const profilesInUse = "another meshstack command is changing the profiles"

func TestAccABrowserLoginHoldsTheProfilesUntilTheBrowserComesBack(t *testing.T) {
	endpoint := requireLocalStack(t)
	login := firstLoginWithAWorkspace(t, devLogins(t))
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

func TestAccALoginFailsWhileMeshstackProfileHoldsTheProfiles(t *testing.T) {
	c := newCLI(t, requireLocalStack(t)).withApiKey()
	c.applyEnv()
	release := testlogin.HoldProfiles(t)

	output, err := c.run("1\n", "login", "--apikey")
	require.Errorf(t, err, "the login ran while meshstack profile held the profiles:\n%s", output)
	assert.Contains(t, err.Error(), "cannot log in while "+profilesInUse+", such as meshstack profile")
	assert.NoFileExists(t, c.credentialsJson())

	release()
	output, err = c.run("1\n", "login", "--apikey")
	require.NoErrorf(t, err, "the login failed once the profiles were free:\n%s", output)
	requireStoredLogin(t, c, output)
}
