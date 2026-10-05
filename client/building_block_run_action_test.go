package client

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

func TestBuildingBlockRunActionClientSendsTheTokenOnlyBelowTheEndpoint(t *testing.T) {
	const runUuid = "a0000000-0000-4000-8000-000000000001"
	server := fakemeshstack.Start(t, fakemeshstack.Options{BuildingBlockRuns: []any{
		jsontext.Value(`{"metadata": {"uuid": "` + runUuid + `"}, "status": "IN_PROGRESS"}`),
	}})
	httpClient := newTestHttpClient(server)
	actions := newBuildingBlockRunActionClient(httpClient, newBuildingBlockRunClient(t.Context(), httpClient))

	t.Run("a link below the endpoint is followed", func(t *testing.T) {
		run, err := actions.ReadRun(t.Context(), Link{Href: server.URL + "/api/meshobjects/meshbuildingblockruns/" + runUuid})

		require.NoError(t, err)
		assert.Equal(t, runUuid, run.Metadata.Uuid)
		server.TakeRequests()
	})

	t.Run("a link to another host is refused before anything is sent", func(t *testing.T) {
		for _, href := range []string{"https://other.host/api/meshobjects/meshbuildingblockruns/" + runUuid, "//other.host/x"} {
			err := actions.AbortRun(t.Context(), Link{Href: href})

			require.ErrorContains(t, err, "which is not below the endpoint")
		}
		assert.Empty(t, server.TakeRequests())
	})
}
