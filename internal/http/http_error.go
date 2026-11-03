package http

import (
	"encoding/json/v2"
	"fmt"
	gohttp "net/http"
	"strings"
)

type Error struct {
	StatusCode   int
	ResponseBody []byte
}

// Error describes the response from its fields, with the clientVersionHint on a line of its own. A
// response of another format is shown as it came.
func (e Error) Error() string {
	body, ok := e.body()
	if !ok {
		return fmt.Sprintf("%s, but the response is not in the expected error format: '%s'", e.Summary(), string(e.ResponseBody))
	}
	if body.ClientVersionHint != "" {
		return e.Summary() + "\n" + body.ClientVersionHint
	}
	return e.Summary()
}

// Summary is Error without the clientVersionHint and without a body of an unexpected format.
func (e Error) Summary() string {
	summary := fmt.Sprintf("meshStack answered HTTP %d", e.StatusCode)
	body, ok := e.body()
	if !ok {
		return summary
	}
	if body.Message != "" {
		summary += ": " + body.Message
	}
	var details []string
	if body.ErrorCode != "" {
		details = append(details, "errorCode "+body.ErrorCode)
	}
	if body.ErrorId != "" {
		details = append(details, "errorId "+body.ErrorId)
	}
	if len(details) > 0 {
		summary += " (" + strings.Join(details, ", ") + ")"
	}
	return summary
}

// errorBody is the body of every error response of meshStack, the 401 of a request without
// credentials included.
type errorBody struct {
	ErrorCode         string `json:"errorCode"`
	Message           string `json:"message"`
	ErrorId           string `json:"errorId"`
	ClientVersionHint string `json:"clientVersionHint"`
}

func (e Error) ErrorCode() string {
	body, _ := e.body()
	return body.ErrorCode
}

// ClientVersionHint is what meshStack tells a client that is too old for it, and is empty for any other
// response.
func (e Error) ClientVersionHint() string {
	body, _ := e.body()
	return body.ClientVersionHint
}

func (e Error) IsClientError() bool {
	return e.StatusCode >= gohttp.StatusBadRequest && e.StatusCode < gohttp.StatusInternalServerError
}

func (e Error) IsUnauthorized() bool {
	return e.StatusCode == gohttp.StatusUnauthorized
}

func (e Error) IsForbidden() bool {
	return e.StatusCode == gohttp.StatusForbidden
}

func (e Error) IsNotFound() bool {
	return e.StatusCode == gohttp.StatusNotFound
}

func (e Error) IsConflict() bool {
	return e.StatusCode == gohttp.StatusConflict
}

func (e Error) IsLocked() bool {
	return e.StatusCode == gohttp.StatusLocked
}

func (e Error) body() (body errorBody, ok bool) {
	if err := json.Unmarshal(e.ResponseBody, &body); err != nil {
		return errorBody{}, false
	}
	return body, body.ErrorCode != "" || body.Message != ""
}
