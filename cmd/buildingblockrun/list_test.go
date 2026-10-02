package buildingblockrun_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/buildingblockrun"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestListOfEveryBuildingBlockMergesTheirRunsNewestFirst(t *testing.T) {
	const page = `"page": {"totalPages": 1, "number": 0}`
	runPagesOf := map[string]string{
		"b0000000-0000-4000-8000-00000000000a": `{"_embedded": {"meshBuildingBlockRuns": [
			{"metadata": {"uuid": "a-newest", "createdAt": "2026-10-01T05:00:00Z"}},
			{"metadata": {"uuid": "a-oldest", "createdAt": "2026-10-01T01:00:00Z"}}
		]}, ` + page + `}`,
		"b0000000-0000-4000-8000-00000000000b": `{"_embedded": {"meshBuildingBlockRuns": [
			{"metadata": {"uuid": "b-newest", "createdAt": "2026-10-01T04:00:00Z"}},
			{"metadata": {"uuid": "b-oldest", "createdAt": "2026-10-01T02:00:00Z"}}
		]}, ` + page + `}`,
	}
	var runPageSizes []string
	meshStack := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		switch r.URL.Path {
		case "/api/meshobjects/meshbuildingblocks":
			_, _ = io.WriteString(w, `{"_embedded": {"meshBuildingBlocks": [
				{"metadata": {"uuid": "b0000000-0000-4000-8000-00000000000a"}},
				{"metadata": {"uuid": "b0000000-0000-4000-8000-00000000000b"}}
			]}, `+page+`}`)
		case "/api/meshobjects/meshbuildingblockruns":
			runPageSizes = append(runPageSizes, r.URL.Query().Get("size"))
			_, _ = io.WriteString(w, runPagesOf[r.URL.Query().Get("buildingBlockUuid")])
		default:
			gohttp.NotFound(w, r)
		}
	}))
	t.Cleanup(meshStack.Close)
	testlogin.LoggedInTo(t, meshStack.URL)

	cmd := buildingblockrun.New()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "-o", "ndjson"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	var listed []string
	for line := range strings.Lines(stdout.String()) {
		var run struct {
			Metadata struct {
				Uuid string `json:"uuid"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &run))
		listed = append(listed, run.Metadata.Uuid)
	}
	assert.Equal(t, []string{"a-newest", "b-newest", "b-oldest", "a-oldest"}, listed)
	assert.Equal(t, []string{"10", "10"}, runPageSizes, "the first page of every block is read before a run is listed, so it is small")
}
