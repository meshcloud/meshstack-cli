package eventlog_test

import (
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/eventlog"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestTheFiltersOfAnEventLogList(t *testing.T) {
	var queries []url.Values
	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		queries = append(queries, r.URL.Query())
		_, _ = io.WriteString(w, `{"_embedded": {"meshEventLogs": []}, "page": {"totalPages": 1, "number": 0}}`)
	}))
	t.Cleanup(server.Close)
	testlogin.LoggedInTo(t, server.URL)
	list := func(t *testing.T, args ...string) error {
		t.Helper()
		queries = nil
		cmd := eventlog.New()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"list"}, args...))
		return cmd.ExecuteContext(t.Context())
	}

	t.Run("go as query parameters, a date as its midnight in UTC, and the workspace from the environment", func(t *testing.T) {
		t.Setenv("MESHSTACK_WORKSPACE", "my-workspace")

		require.NoError(t, list(t,
			"--title", "Workspace Created",
			"--exclude-title", "Tenant Replicated",
			"--exclude-title", "Tenant Quota Changed, Again",
			"--from", "2026-07-01",
			"--until", "2026-08-01T12:30:00+02:00",
		))

		require.Len(t, queries, 1)
		query := queries[0]
		assert.Equal(t, "Workspace Created", query.Get("title"))
		assert.Equal(t, []string{"Tenant Replicated", "Tenant Quota Changed, Again"}, query["excludeTitle"], "a comma splits no title")
		assert.Equal(t, "2026-07-01T00:00:00Z", query.Get("from"))
		assert.Equal(t, "2026-08-01T12:30:00+02:00", query.Get("until"))
		assert.Equal(t, "my-workspace", query.Get("workspaceIdentifier"))
		assert.Equal(t, "createdAt,desc", query.Get("sort"))
	})

	t.Run("refuse a malformed date or an empty range before the list asks meshStack", func(t *testing.T) {
		for args, wantError := range map[string]string{
			"--from 01.07.2026":                    `"01.07.2026" is no date or instant`,
			"--from 2026-07-02 --until 2026-07-01": "--from 2026-07-02T00:00:00Z is not before --until 2026-07-01T00:00:00Z, so no event log can match",
			"--from 2026-07-01 --until 2026-07-01": "is not before --until",
		} {
			require.ErrorContains(t, list(t, strings.Fields(args)...), wantError, args)
			assert.Empty(t, queries, args)
		}
	})
}
