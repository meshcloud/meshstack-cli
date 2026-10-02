package testacc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLineage marks the state this suite stores, so that a later run may delete one an aborted run
// left behind, and no other.
const testLineage = "meshstack-cli-testacc"

const noState = "has no state in workspace"

// tfstateStoresAndReadsAState needs the ADM_TFSTATE rights of the API key of this suite, which reach
// every workspace. curl stands in for tofu's http backend, which sends the same requests.
func tfstateStoresAndReadsAState(c *cli) func(*testing.T) {
	return func(t *testing.T) {
		buildingBlock := buildingBlockWithoutState(t, c)

		const minimalState = `{"version":4,"serial":1,"lineage":"` + testLineage + `"}`
		const script = `state() { curl --silent --show-error --fail --user "$TF_HTTP_USERNAME:$TF_HTTP_PASSWORD" "$@" "$TF_HTTP_ADDRESS"; }
state --request DELETE || true
state --data '` + minimalState + `' >/dev/null
echo "read $(state)"
state --request DELETE`
		output, err := c.run("", "buildingblock", "tfstate", "exec", buildingBlock, "--mode", "readwrite", "--", "sh", "-ec", script)
		require.NoErrorf(t, err, "the exec failed:\n%s", output)
		assert.Contains(t, output, "read "+minimalState, "the GET reads the state the POST stored")
		assert.Contains(t, output, "Saved the stored state of building block "+buildingBlock, "the DELETE backs the state up first")

		output, err = c.run("", "buildingblock", "tfstate", "show", buildingBlock)
		require.NoErrorf(t, err, "the show failed:\n%s", output)
		assert.Contains(t, output, noState, "the DELETE through the proxy removed the state")
	}
}

// buildingBlockWithoutState skips a building block whose state is not this suite's to delete, and
// one whose run may write the state while this test does, such as the run trigger-run started.
func buildingBlockWithoutState(t *testing.T, c *cli) string {
	t.Helper()
	output, err := c.run("", "buildingblock", "list", "--limit", "unlimited", "-o", "ndjson")
	require.NoErrorf(t, err, "the building block list failed:\n%s", output)
	blocks := ndjsonObjects[struct {
		Metadata struct {
			Uuid string `json:"uuid"`
		} `json:"metadata"`
		Status struct {
			Status string `json:"status"`
		} `json:"status"`
	}](output)
	for _, block := range blocks {
		if block.Status.Status == "IN_PROGRESS" || block.Status.Status == "PENDING" {
			continue
		}
		output, err := c.run("", "buildingblock", "tfstate", "show", block.Metadata.Uuid)
		require.NoErrorf(t, err, "the show failed:\n%s", output)
		if strings.Contains(output, noState) || strings.Contains(output, `"lineage":"`+testLineage+`"`) {
			return block.Metadata.Uuid
		}
	}
	t.Skipf("none of the %d building blocks of the local stack is settled and has no state", len(blocks))
	return ""
}

// tfstateTakesAnApiKey finds a building block with an API key, which reaches every workspace: the
// building block list of the organization admin answers none on the local stack.
func tfstateTakesAnApiKey(browserLogin, apiKey *cli) func(*testing.T) {
	return func(t *testing.T) {
		output, err := apiKey.run("", "login", "--apikey")
		require.NoErrorf(t, err, "the API key login did not finish:\n%s", output)
		output, err = apiKey.run("", "buildingblock", "list", "--limit", "1", "-o", "ndjson")
		require.NoErrorf(t, err, "the building block list failed:\n%s", output)
		buildingBlock := firstUuid(output)
		if buildingBlock == "" {
			t.Skip("the local stack holds no building block")
		}

		output, err = browserLogin.run("", "buildingblock", "tfstate", "show", buildingBlock)

		require.Errorf(t, err, "meshStack let a browser login read the state:\n%s", output)
		assert.Contains(t, err.Error(), "http error 403")
		assert.Contains(t, err.Error(), "This command needs an API key login, meshstack login --apikey")
	}
}
