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

	t.Run("wait needs the building block from before the write", func(t *testing.T) {
		_, err := run("", "wait", waitedBuildingBlock)
		require.ErrorContains(t, err, `required flag(s) "before" not set`)
	})

	t.Run("a failed run fails the wait with its action and the meshPanel link", func(t *testing.T) {
		current.Store(new(buildingBlockWithRun("REVIEW_ERRORS", "run-2")))
		stdout, err := run(before, "wait", waitedBuildingBlock, "--before=-")
		require.ErrorContains(t, err, "ended with action REVIEW_ERRORS")
		require.ErrorContains(t, err, "https://panel.example/#/go/buildingblock/"+waitedBuildingBlock)
		assert.Contains(t, stdout, `"action": "REVIEW_ERRORS"`, "the building block still goes to stdout")
	})

	t.Run("a run that waits for approval fails the wait", func(t *testing.T) {
		current.Store(new(buildingBlockWithRun("APPROVE_RUN", "run-2")))
		_, err := run(before, "wait", waitedBuildingBlock, "--before=-")
		require.ErrorContains(t, err, "waits with action APPROVE_RUN")
	})
}
