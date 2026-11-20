package client

import (
	"errors"
	"fmt"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/http"
)

// MeshObjectError is the body of every error response of meshStack, the 401 of a request without
// credentials included.
type MeshObjectError struct {
	Http              http.Error `json:"-"`
	ErrorCode         string     `json:"errorCode"`
	Message           string     `json:"message"`
	ErrorId           string     `json:"errorId"`
	ClientVersionHint string     `json:"clientVersionHint"`

	// err keeps what wraps the http.Error, such as the authorization of a 403.
	err error
}

// MeshObjectErrorOf turns an error that holds an http.Error into a MeshObjectError, and returns any
// other error as it is.
func MeshObjectErrorOf(err error) error {
	httpErr, ok := errors.AsType[http.Error](err)
	if !ok {
		return err
	}
	meshErr, _ := httpErr.BodyAsJson[MeshObjectError]()
	meshErr.Http, meshErr.err = httpErr, err
	return meshErr
}

// Error puts the clientVersionHint on a line of its own.
func (e MeshObjectError) Error() string {
	message := fmt.Sprintf("meshStack answered HTTP %d", e.Http.StatusCode)
	if e.Message != "" {
		message += ": " + e.Message
	}
	var details []string
	if e.ErrorCode != "" {
		details = append(details, "errorCode "+e.ErrorCode)
	}
	if e.ErrorId != "" {
		details = append(details, "errorId "+e.ErrorId)
	}
	if len(details) > 0 {
		message += " (" + strings.Join(details, ", ") + ")"
	}
	if e.ClientVersionHint != "" {
		message += "\n" + e.ClientVersionHint
	}
	if e.Http.Method != "" {
		message = e.Http.Method + " " + e.Http.Url + ": " + message
	}
	return strings.Replace(e.err.Error(), e.Http.Error(), message, 1)
}

func (e MeshObjectError) Unwrap() error {
	return e.err
}
