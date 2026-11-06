package buildingblock_test

import (
	"bytes"
	"fmt"
	gohttp "net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestLockAndUnlock(t *testing.T) {
	const buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000004"
	var action atomic.Pointer[string]
	action.Store(new("NO_ACTION"))
	writeBlock := func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = fmt.Fprintf(w, `{"metadata": {"uuid": %q, "ownedByWorkspace": "my-workspace"}, "spec": {}, "status": {"action": %q}}`,
			buildingBlockUuid, *action.Load())
	}
	meshStack := fakemeshstack.Start(t, fakemeshstack.Options{})
	meshStack.Route("GET /api/meshobjects/meshbuildingblocks/{uuid}", writeBlock)
	meshStack.Route("POST /api/meshobjects/meshbuildingblocks/{uuid}/lock", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		action.Store(new("UNLOCK_RUNS"))
		writeBlock(w, r)
	})
	meshStack.Route("POST /api/meshobjects/meshbuildingblocks/{uuid}/unlock", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		action.Store(new("PENDING"))
		writeBlock(w, r)
	})
	testlogin.LoggedInTo(t, meshStack.URL)

	run := func(t *testing.T, command string) string {
		t.Helper()
		cmd := buildingblock.New()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{command, buildingBlockUuid, "-o", "ndjson"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		return stdout.String()
	}
	posted := func(t *testing.T) []string {
		t.Helper()
		var paths []string
		for _, request := range meshStack.TakeRequests() {
			if request.Method == gohttp.MethodPost {
				paths = append(paths, request.URL.Path)
			}
		}
		return paths
	}

	t.Run("lock writes the locked building block", func(t *testing.T) {
		assert.Contains(t, run(t, "lock"), `"action":"UNLOCK_RUNS"`)
		assert.Equal(t, []string{"/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid + "/lock"}, posted(t))
	})

	t.Run("unlock writes the building block as meshStack left it", func(t *testing.T) {
		assert.Contains(t, run(t, "unlock"), `"action":"PENDING"`)
		assert.Equal(t, []string{"/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid + "/unlock"}, posted(t))
	})
}
