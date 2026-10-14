package client

import (
	"context"
	"encoding/json/jsontext"
	"time"

	"github.com/meshcloud/meshstack-cli/client/internal"
)

type MeshEventLog struct {
	Metadata MeshEventLogMetadata `json:"metadata"`
	Spec     MeshEventLogSpec     `json:"spec"`
	Status   MeshEventLogStatus   `json:"status"`
}

type MeshEventLogMetadata struct {
	Uuid string `json:"uuid"`
}

type MeshEventLogSpec struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// EventType is one of Added, Approved, Cancelled, Changed, Created, Deleted, Rejected, Removed
	// and Requested.
	EventType string `json:"eventType"`
	// WorkspaceRef is nil for an event of the whole platform.
	WorkspaceRef *MeshEventLogWorkspaceRef `json:"workspaceRef"`
	// Content is an object of no fixed shape. PreviousContent is null but for a Changed event.
	Content         jsontext.Value `json:"content"`
	PreviousContent jsontext.Value `json:"previousContent"`
}

type MeshEventLogWorkspaceRef struct {
	Kind string `json:"kind"`
	// Name is the workspace identifier.
	Name string `json:"name"`
}

type MeshEventLogStatus struct {
	Created MeshEventLogCreated `json:"created"`
}

type MeshEventLogCreated struct {
	Timestamp time.Time           `json:"timestamp"`
	Author    *MeshEventLogAuthor `json:"author"`
}

type MeshEventLogAuthor struct {
	// Type is one of ApiKey, ApiUser, System and User, and decides what Identifier is: the uuid of
	// the meshApiKey, the name of the API user, "system", or the uuid of the meshUser.
	Type        string  `json:"type"`
	Identifier  string  `json:"identifier"`
	DisplayName *string `json:"displayName"`
	Username    *string `json:"username"`
	Email       *string `json:"email"`
	Euid        *string `json:"euid"`
}

type MeshEventLogListFilter struct {
	From  time.Time `json:"from,omitzero"`
	Until time.Time `json:"until,omitzero"`
	Title string    `json:"title"`
	// ExcludeTitle leaves out the event logs of each title exactly, for an event type of high volume.
	ExcludeTitle     []string `json:"excludeTitle"`
	Description      string   `json:"description"`
	EventType        string   `json:"eventType"`
	AuthorType       string   `json:"authorType"`
	AuthorIdentifier string   `json:"authorIdentifier"`
	// WorkspaceIdentifier and WorkspaceName, which matches part of the display name, exclude
	// each other.
	WorkspaceIdentifier string       `json:"workspaceIdentifier"`
	WorkspaceName       string       `json:"workspaceName"`
	Sort                SortCriteria `json:"sort"`
}

func newEventLogMeshObject(ctx context.Context, httpClient internal.HttpClient) internal.MeshObjectClient[MeshEventLog] {
	return internal.NewMeshObjectClient[MeshEventLog](ctx, httpClient, "v1")
}
