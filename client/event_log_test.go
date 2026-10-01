package client

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnEventLogDecodesAsTheApiDocumentsIt(t *testing.T) {
	var query url.Values
	server := httptest.NewTestServer(t, gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		query = r.URL.Query()
		_, _ = io.WriteString(w, `{
			"_embedded": {"meshEventLogs": [{
				"apiVersion": "v1",
				"kind": "meshEventLog",
				"metadata": {"uuid": "c1d2e3f4-0000-4000-8000-000000000001"},
				"spec": {
					"title": "Workspace Created",
					"description": "Workspace ops was created",
					"eventType": "Created",
					"workspaceRef": {"kind": "meshWorkspace", "name": "ops"},
					"content": {"identifier": "ops"},
					"previousContent": null
				},
				"status": {"created": {
					"timestamp": "2026-07-01T12:00:00Z",
					"author": {"type": "User", "identifier": "u1", "username": "jane", "email": "jane@example.com"}
				}},
				"_links": {}
			}]},
			"page": {"totalPages": 1, "number": 0}
		}`)
	}))
	httpClient := newTestHttpClient(server)
	raw := newRawClient(httpClient).with(newEventLogMeshObject(t.Context(), httpClient))

	var listed []MeshEventLog
	for rawEventLog, err := range raw.List[MeshEventLog](t.Context(), MeshEventLogListFilter{EventType: "Created", WorkspaceIdentifier: "ops"}, ListOptions{}) {
		require.NoError(t, err)
		var eventLog MeshEventLog
		require.NoError(t, json.Unmarshal(rawEventLog, &eventLog))
		listed = append(listed, eventLog)
	}

	assert.Equal(t, []MeshEventLog{{
		Metadata: MeshEventLogMetadata{Uuid: "c1d2e3f4-0000-4000-8000-000000000001"},
		Spec: MeshEventLogSpec{
			Title:           "Workspace Created",
			Description:     "Workspace ops was created",
			EventType:       "Created",
			WorkspaceRef:    &MeshEventLogWorkspaceRef{Kind: "meshWorkspace", Name: "ops"},
			Content:         jsontext.Value(`{"identifier": "ops"}`),
			PreviousContent: jsontext.Value("null"),
		},
		Status: MeshEventLogStatus{Created: MeshEventLogCreated{
			Timestamp: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Author:    &MeshEventLogAuthor{Type: "User", Identifier: "u1", Username: new("jane"), Email: new("jane@example.com")},
		}},
	}}, listed)
	query.Del("until")
	assert.Equal(t, url.Values{
		"eventType":           {"Created"},
		"workspaceIdentifier": {"ops"},
		"sort":                {"createdAt,desc"},
		"page":                {"0"},
	}, query, "an unset filter sends no parameter")
}
