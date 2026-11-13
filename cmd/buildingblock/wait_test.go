package buildingblock_test

import (
	"bytes"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const waitedBuildingBlock = "b1d2c3e4-0000-4000-8000-000000000002"

func buildingBlockWithRun(action, latestRunUuid string) string {
	return fmt.Sprintf(`{"metadata": {"uuid": %q}, "spec": {}, "status": {"action": %q, "latestRunUuid": %q}, `+
		`"_links": {"meshPanel": {"href": "https://panel.example/#/go/buildingblock/%s"}}}`,
		waitedBuildingBlock, action, latestRunUuid, waitedBuildingBlock)
}

func TestWaitFailsOnAnOutcomeThatNeedsSomeoneToAct(t *testing.T) {
	before := buildingBlockWithRun("NO_ACTION", "run-1")
	var current atomic.Pointer[string]
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{})
	meshStack.Route("GET /api/meshobjects/meshbuildingblocks/{uuid}", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = io.WriteString(w, *current.Load())
	})
	testlogin.LoggedInTo(t, meshStack.URL)

	run := func(stdin string, args ...string) (string, error) {
		cmd := buildingblock.New()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(t.Context())
		return stdout.String(), err
	}

	t.Run("without --before, wait prints the building block whose run has ended", func(t *testing.T) {
		current.Store(new(buildingBlockWithRun("NO_ACTION", "run-1")))
		stdout, err := run("", "wait", waitedBuildingBlock)
		require.NoError(t, err)
		assert.Contains(t, stdout, `"action": "NO_ACTION"`)
	})

	t.Run("a failed run fails the wait with its action and the meshPanel link", func(t *testing.T) {
		current.Store(new(buildingBlockWithRun("REVIEW_ERRORS", "run-2")))
		stdout, err := run(before, "wait", waitedBuildingBlock, "--before=-")
		require.ErrorContains(t, err, "ended with action REVIEW_ERRORS")
		require.ErrorContains(t, err, "https://panel.example/#/go/buildingblock/"+waitedBuildingBlock)
		assert.Contains(t, stdout, `"action": "REVIEW_ERRORS"`, "the building block still goes to stdout")
	})

	t.Run("a run that waits for approval fails the wait and names the waiting and the detect run", func(t *testing.T) {
		current.Store(new(strings.Replace(buildingBlockWithRun("APPROVE_RUN", "run-2"), `"latestRunUuid"`, `"latestDryRunUuid": "run-1", "latestRunUuid"`, 1)))
		_, err := run(before, "wait", waitedBuildingBlock, "--before=-")
		require.ErrorContains(t, err, "run run-2 of building block "+waitedBuildingBlock+" is waiting for approval of the plan from the detect run run-1")
	})
}
