package eventlog_test

import (
	"bytes"
	"encoding/base64"
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
)

func TestAListSendsItsFiltersOldestFirstAndPagesToTheEnd(t *testing.T) {
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
			"_embedded": {"meshEventLogs": [{"metadata": {"uuid": "event-%d"}}]},
			"page": {"size": 1, "totalElements": 2, "totalPages": 2, "number": %[1]d}
		}`, page)
	}))
	t.Cleanup(server.Close)
	loggedInTo(t, server.URL)
	t.Setenv("MESHSTACK_WORKSPACE", "my-workspace")
	var stdout bytes.Buffer
	cmd := eventlog.New()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{
		"list",
		"--title", "Workspace Created",
		"--from", "2026-07-01",
		"--until", "2026-08-01T12:30:00+02:00",
		"--limit", "unlimited",
	})

	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.JSONEq(t, `[{"metadata":{"uuid":"event-0"}},{"metadata":{"uuid":"event-1"}}]`, stdout.String())
	require.Len(t, queries, 2)
	for _, query := range queries {
		assert.Equal(t, "Workspace Created", query.Get("title"))
		assert.Equal(t, "2026-07-01T00:00:00Z", query.Get("from"))
		assert.Equal(t, "2026-08-01T12:30:00+02:00", query.Get("until"))
		assert.Equal(t, "my-workspace", query.Get("workspaceIdentifier"))
		assert.Equal(t, "createdAt,asc", query.Get("sort"))
	}
}

func TestAListRejectsAMalformedDateBeforeItAsksMeshStack(t *testing.T) {
	server := httptest.NewServer(gohttp.HandlerFunc(func(gohttp.ResponseWriter, *gohttp.Request) {
		t.Error("the list asked meshStack although --from does not parse")
	}))
	t.Cleanup(server.Close)
	loggedInTo(t, server.URL)
	cmd := eventlog.New()
	cmd.SetArgs([]string{"list", "--from", "01.07.2026"})

	err := cmd.ExecuteContext(t.Context())

	assert.ErrorContains(t, err, `"01.07.2026" is no date or instant`)
}

// loggedInTo points the CLI at endpoint with a token that never expires: one without an expiry
// counts as expired, and would be refreshed before the first request.
func loggedInTo(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("MESHSTACK_ENDPOINT", endpoint)
	t.Setenv("MESHSTACK_API_TOKEN", "e30."+base64.RawURLEncoding.EncodeToString([]byte(`{"exp":4102444800}`))+".signature")
	t.Setenv("MESHSTACK_SKIP_VERSION_CHECK", "true")
	t.Setenv("MESHSTACK_CONFIG_DIR", t.TempDir())
}
