package buildingblock_test

import (
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblock"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const (
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
	triggerRunPath    = "/api/meshobjects/meshbuildingblocks/" + buildingBlockUuid + "/trigger-run"
)

func TestTriggerRunOnlyTriggersTheRun(t *testing.T) {
	var requests []string
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests = append(requests, r.Method+" "+r.URL.Path+" "+string(body))
		if r.Method != gohttp.MethodPost || r.URL.Path != triggerRunPath {
			gohttp.NotFound(w, r)
			return
		}
		w.WriteHeader(gohttp.StatusAccepted)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	cmd.SetArgs([]string{"trigger-run", buildingBlockUuid})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	require.Len(t, requests, 1, "the command triggers the run and does nothing else, such as waiting for it")
	assert.Contains(t, requests[0], "POST "+triggerRunPath)
	assert.Contains(t, requests[0], `"dryRun":false`)
}

func TestTriggerRunRejectsANameOfNoUuidBeforeItAsksMeshStack(t *testing.T) {
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(gohttp.ResponseWriter, *gohttp.Request) {
		t.Error("the command asked meshStack about a building block of no uuid")
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblock.New()
	cmd.SetArgs([]string{"trigger-run", "my-building-block"})

	assert.EqualError(t, cmd.ExecuteContext(t.Context()), `"my-building-block" is no uuid`)
}
