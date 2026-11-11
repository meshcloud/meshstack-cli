package client

import (
	"encoding/json/jsontext"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/hal"
)

type MeshEventLog struct {
	Metadata MeshEventLogMetadata `json:"metadata"`
	Spec     MeshEventLogSpec     `json:"spec"`
	Status   MeshEventLogStatus   `json:"status"`
	Links    hal.Links            `json:"_links,omitzero"`
}

type MeshEventLogMetadata struct {
	Uuid *string `json:"uuid"`
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
	Timestamp time.Time         `json:"timestamp"`
	Author    *MeshObjectAuthor `json:"author"`
}

type MeshEventLogFilter struct {
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
	WorkspaceIdentifier string `json:"workspaceIdentifier"`
	WorkspaceName       string `json:"workspaceName"`
}

type MeshEventLogClient = MeshObjectClient[MeshEventLog, MeshEventLogFilter]

func (log MeshEventLog) meshObjectId() string {
	return *log.Metadata.Uuid
}
