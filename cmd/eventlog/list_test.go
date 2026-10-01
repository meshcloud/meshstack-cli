package eventlog_test

import (
	"bytes"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/eventlog"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestAListSendsItsFiltersNewestFirstAndPagesToTheEnd(t *testing.T) {
	var queries []url.Values
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		assert.Equal(t, "/api/meshobjects/mesheventlogs", r.URL.Path)
		// Media types compare case-insensitively, and the client spells the kind in camelCase.
		assert.Equal(t, "application/vnd.meshcloud.api.mesheventlog.v1.hal+json", strings.ToLower(r.Header.Get("Accept")))
		queries = append(queries, r.URL.Query())
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(t, err) {
			w.WriteHeader(gohttp.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `
		{
			"_embedded": {"meshEventLogs": [{
				"apiVersion": "v1", "kind": "meshEventLog", "metadata": {"uuid": "event-%d"}, "_links": {"self": {"href": "x"}}
			}]},
			"page": {"size": 1, "totalElements": 2, "totalPages": 2, "number": %[1]d}
		}`, page)
	}))
	t.Cleanup(server.Close)
	testlogin.LoggedInTo(t, server.URL)
	t.Setenv("MESHSTACK_WORKSPACE", "my-workspace")
	var stdout bytes.Buffer
	cmd := eventlog.New()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"list",
		"--title", "Workspace Created",
		"--exclude-title", "Tenant Replicated",
		"--exclude-title", "Tenant Quota Changed, Again",
		"--from", "2026-07-01",
		"--until", "2026-08-01T12:30:00+02:00",
		"--limit", "unlimited",
	})

	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.JSONEq(t, `[
		{"apiVersion": "v1", "kind": "meshEventLog", "metadata": {"uuid": "event-0"}, "_links": {"self": {"href": "x"}}},
		{"apiVersion": "v1", "kind": "meshEventLog", "metadata": {"uuid": "event-1"}, "_links": {"self": {"href": "x"}}}
	]`, stdout.String(), "each event is written as meshStack sent it")
	require.Len(t, queries, 2)
	for _, query := range queries {
		assert.Equal(t, "Workspace Created", query.Get("title"))
		assert.Equal(t, []string{"Tenant Replicated", "Tenant Quota Changed, Again"}, query["excludeTitle"])
		assert.Equal(t, "2026-07-01T00:00:00Z", query.Get("from"))
		assert.Equal(t, "2026-08-01T12:30:00+02:00", query.Get("until"))
		assert.Equal(t, "my-workspace", query.Get("workspaceIdentifier"))
		assert.Equal(t, "createdAt,desc", query.Get("sort"))
	}
}

func TestAListRejectsAMalformedDateOrAnEmptyRangeBeforeItAsksMeshStack(t *testing.T) {
	server := httptest.NewServer(gohttp.HandlerFunc(func(gohttp.ResponseWriter, *gohttp.Request) {
		t.Error("the list asked meshStack although its flags select nothing")
	}))
	t.Cleanup(server.Close)
	testlogin.LoggedInTo(t, server.URL)
	for args, wantError := range map[string]string{
		"--from 01.07.2026":                    `"01.07.2026" is no date or instant`,
		"--from 2026-07-02 --until 2026-07-01": "--from 2026-07-02T00:00:00Z is not before --until 2026-07-01T00:00:00Z, so no event log can match",
		"--from 2026-07-01 --until 2026-07-01": "is not before --until",
	} {
		cmd := eventlog.New()
		cmd.SetArgs(append([]string{"list"}, strings.Fields(args)...))

		err := cmd.ExecuteContext(t.Context())

		assert.ErrorContains(t, err, wantError, args)
	}
}
