package buildingblock

import (
	"io"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestUpgradeSendsOnlyTheNewVersionAndTheGivenInputs(t *testing.T) {
	const (
		currentVersion = "c0000000-0000-4000-8000-000000000001"
		latestRelease  = "c0000000-0000-4000-8000-000000000002"
		definition     = "d0000000-0000-4000-8000-000000000001"
	)
	server := fakemeshstack.Start(t, fakemeshstack.Options{
		BuildingBlocks: []any{meshObject(t, `{"apiVersion": "v2-preview", "kind": "meshBuildingBlock", `+
			`"metadata": {"uuid": "`+buildingBlockUuid+`", "ownedByWorkspace": "app"}, `+
			`"spec": {"displayName": "my-block", "buildingBlockDefinitionVersionRef": {"kind": "meshBuildingBlockDefinitionVersion", "uuid": "`+currentVersion+`"}, `+
			`"targetRef": {"kind": "meshWorkspace", "name": "app"}, "parentBuildingBlockRefs": [], `+
			`"inputs": {"name": {"value": "web", "assignmentType": "USER_INPUT"}, "hub": {"value": "eu", "assignmentType": "STATIC"}}}, `+
			`"status": {"action": "UPGRADE_AVAILABLE"}}`)},
	})
	server.Route("GET /api/meshobjects/meshbuildingblockdefinitionversions/"+currentVersion, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = io.WriteString(w, `{"metadata": {"uuid": "`+currentVersion+`"}, "spec": {"buildingBlockDefinitionRef": {"kind": "meshBuildingBlockDefinition", "uuid": "`+definition+`"}}}`)
	})
	server.Route("GET /api/meshobjects/meshbuildingblockdefinitions/"+definition, func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		_, _ = io.WriteString(w, `{"metadata": {"uuid": "`+definition+`"}, "status": {"latestReleasedVersionUuid": "`+latestRelease+`"}}`)
	})
	var sent string
	server.Route("PUT /api/meshobjects/meshbuildingblocks/"+buildingBlockUuid, func(w gohttp.ResponseWriter, r *gohttp.Request) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)
		_, _ = w.Write(body)
	})
	testlogin.LoggedInTo(t, server.URL)

	cmd := New()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"upgrade", buildingBlockUuid, "--input", `payment_method={"kind": "meshPaymentMethod", "name": "pm"}`, "--input", "size=small"})

	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.JSONEq(t, `{"apiVersion": "v2-preview", "kind": "meshBuildingBlock", "metadata": {"uuid": "`+buildingBlockUuid+`", "ownedByWorkspace": "app"}, `+
		`"spec": {"displayName": "my-block", "buildingBlockDefinitionVersionRef": {"kind": "meshBuildingBlockDefinitionVersion", "uuid": "`+latestRelease+`"}, `+
		`"targetRef": {"kind": "meshWorkspace", "name": "app"}, "parentBuildingBlockRefs": [], `+
		`"inputs": {"payment_method": {"value": {"kind": "meshPaymentMethod", "name": "pm"}}, "size": {"value": "small"}}}}`, sent)
}
