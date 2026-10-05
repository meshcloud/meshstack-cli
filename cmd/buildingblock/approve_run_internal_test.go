package buildingblock

import (
	"bytes"
	"encoding/json/v2"
	"io"
	gohttp "net/http"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/prompt"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

const (
	runsPath          = "/api/meshobjects/meshbuildingblockruns/"
	buildingBlockUuid = "b1d2c3e4-0000-4000-8000-000000000001"
)

func meshObject(t *testing.T, content string) map[string]any {
	t.Helper()
	var object map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &object))
	return object
}

func removeLink(t *testing.T, object map[string]any, rel string) {
	t.Helper()
	links, ok := object["_links"].(map[string]any)
	require.True(t, ok)
	delete(links, rel)
}

// newTestRunAction answers the prompt line by line, as no test has a terminal.
func newTestRunAction(t *testing.T, server *fakemeshstack.Server, answers string) (runAction, *bytes.Buffer) {
	t.Helper()
	testlogin.LoggedInTo(t, server.URL)
	meshStack, err := internal.ResolveClient(t.Context())
	require.NoError(t, err)
	var asked bytes.Buffer
	return runAction{
		meshStack: meshStack, prompt: prompt.New(strings.NewReader(answers), &asked), buildingBlockUuid: uuid.MustParse(buildingBlockUuid),
		out: io.Discard, format: internal.OutputJson,
	}, &asked
}

func TestApproveRunApprovesOnlyThePlanItShowed(t *testing.T) {
	const (
		waitingRun = "a0000000-0000-4000-8000-000000000002"
		planRun    = "a0000000-0000-4000-8000-000000000001"
	)
	run := meshObject(t, `{"metadata": {"uuid": "`+waitingRun+`"}, "status": "IN_PROGRESS", `+
		`"spec": {"runNumber": 2, "behavior": "APPLY", "buildingBlock": {"uuid": "`+buildingBlockUuid+`", "spec": {"displayName": "my-block"}}}, `+
		`"_links": {"predecessor": {"href": "`+runsPath+planRun+`"}, "approve": {"href": "`+runsPath+waitingRun+`/approve"}}}`)
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{meshObject(t, `{"metadata": {"uuid": "`+buildingBlockUuid+`"}, "status": {"status": "WAITING_FOR_APPROVAL"}, `+
			`"_links": {"latestRun": {"href": "`+runsPath+waitingRun+`"}}}`)},
		BuildingBlockRuns: []any{run, meshObject(t, `{"metadata": {"uuid": "`+planRun+`"}, "status": "SUCCEEDED", `+
			`"_links": {"downloadLogs": {"href": "`+runsPath+planRun+`/logs"}}}`)},
	})
	server.Route("GET "+runsPath+planRun+"/logs", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = io.WriteString(w, `{"steps": [{"displayName": "Plan", "status": "SUCCEEDED", "userMessage": "+ resource \"x\" will be created"}]}`)
	})
	var approvals []string
	approveAnswer := gohttp.StatusAccepted
	server.Route("POST "+runsPath+waitingRun+"/approve", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, _ := io.ReadAll(r.Body)
		approvals = append(approvals, string(body))
		w.WriteHeader(approveAnswer)
	})

	t.Run("without a terminal, nothing is asked and nothing is approved", func(t *testing.T) {
		testlogin.LoggedInTo(t, server.URL)
		cmd := New()
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"approve-run", buildingBlockUuid})

		require.EqualError(t, cmd.ExecuteContext(t.Context()), "approve-run asks you before it acts, so it runs only on a terminal")
		assert.Empty(t, server.TakeRequests())
	})

	t.Run("a plan that is not approved stays waiting", func(t *testing.T) {
		action, asked := newTestRunAction(t, server, "n\n")

		require.NoError(t, action.approve(t.Context()))
		assert.Contains(t, asked.String(), `Plan | + resource "x" will be created`)
		assert.True(t, strings.HasSuffix(asked.String(), "Approve this plan? [y/N]: "), asked.String())
		assert.Empty(t, approvals)
	})

	t.Run("an approval names the run whose plan was shown", func(t *testing.T) {
		action, _ := newTestRunAction(t, server, "y\n")

		require.NoError(t, action.approve(t.Context()))
		require.Len(t, approvals, 1)
		assert.JSONEq(t, `{"predecessorRunUuid": "`+planRun+`"}`, approvals[0])
	})

	t.Run("a plan that changed after it was shown has to be reviewed again", func(t *testing.T) {
		approveAnswer = gohttp.StatusConflict
		action, _ := newTestRunAction(t, server, "y\n")

		require.EqualError(t, action.approve(t.Context()), "the plan of run "+waitingRun+" changed after you saw it. Run the command again to review the new plan")
	})

	t.Run("a run without an approve link shows no plan", func(t *testing.T) {
		removeLink(t, run, "approve")
		action, asked := newTestRunAction(t, server, "y\n")

		require.ErrorContains(t, action.approve(t.Context()), "Its latest run "+waitingRun+" does not wait for approval")
		assert.Empty(t, asked.String())
	})
}
