package client

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"time"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// MeshEventLog has no fields because event logs are only listed raw. The type names the kind.
type MeshEventLog struct{}

type MeshEventLogListFilter struct {
	From                *time.Time `json:"from"`
	Until               *time.Time `json:"until"`
	Title               *string    `json:"title"`
	WorkspaceIdentifier *string    `json:"workspaceIdentifier"`
}

type meshEventLogClient struct {
	meshObject internal.MeshObjectClient[MeshEventLog]
}

func newEventLogClient(ctx context.Context, httpClient internal.HttpClient) meshEventLogClient {
	return meshEventLogClient{
		meshObject: internal.NewMeshObjectClient[MeshEventLog](ctx, httpClient, "v1"),
	}
}

// oldestFirst makes a listing that pages while new events arrive safe: a new event lands on the
// last page and does not move the events of a page that is still to come.
var oldestFirst = map[string]any{"sort": "createdAt,asc"}

func (c meshEventLogClient) ListRawSeq(ctx context.Context, filter MeshEventLogListFilter) iter.Seq2[jsontext.Value, error] {
	return c.meshObject.ListSeqAs[jsontext.Value](ctx, http.WithUrlQuery(filter), http.WithUrlQuery(oldestFirst))
}
