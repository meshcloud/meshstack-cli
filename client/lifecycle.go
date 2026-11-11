package client

import (
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type MeshLifecycleState string

var (
	MeshLifecycleStates                 = enum.Enum[MeshLifecycleState]{}
	MeshLifecycleStateActive            = MeshLifecycleStates.Entry("ACTIVE")
	MeshLifecycleStateDeactivated       = MeshLifecycleStates.Entry("DEACTIVATED")
	MeshLifecycleStateMarkedForDeletion = MeshLifecycleStates.Entry("MARKED_FOR_DELETION")
	MeshLifecycleStateDeleted           = MeshLifecycleStates.Entry("DELETED")
)

type MeshLifecycle struct {
	State             enum.Entry[MeshLifecycleState] `json:"state"`
	Created           MeshLifecycleTransition        `json:"created"`
	Deactivated       *MeshLifecycleTransition       `json:"deactivated,omitzero"`
	MarkedForDeletion *MeshLifecycleTransition       `json:"markedForDeletion,omitzero"`
	Deleted           *MeshLifecycleTransition       `json:"deleted,omitzero"`
}

type MeshLifecycleTransition struct {
	Timestamp time.Time         `json:"timestamp"`
	Author    *MeshObjectAuthor `json:"author,omitzero"`
}

type MeshObjectAuthor struct {
	// Type is one of ApiKey, ApiUser, System and User, and decides what Identifier is: the uuid of
	// the meshApiKey, the name of the API user, "system", or the uuid of the meshUser.
	Type        string  `json:"type"`
	Identifier  string  `json:"identifier"`
	DisplayName *string `json:"displayName,omitzero"`
	Username    *string `json:"username,omitzero"`
	Email       *string `json:"email,omitzero"`
	Euid        *string `json:"euid,omitzero"`
}
