package http

import (
	"fmt"
	gohttp "net/http"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

type Error struct {
	Method, Url  string
	StatusCode   int
	ResponseBody []byte
}

// Error shows the body as it came, as only the caller knows the format of an error body.
func (e Error) Error() string {
	message := fmt.Sprintf("HTTP %d", e.StatusCode)
	if body := strings.TrimSpace(string(e.ResponseBody)); body != "" {
		message += ": " + body
	}
	if e.Method == "" {
		return message
	}
	return e.Method + " " + e.Url + ": " + message
}

func (e Error) BodyAsJson[T any]() (out T, err error) {
	err = json.Unmarshal(e.ResponseBody, &out)
	return
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

func (e Error) IsTemporary() bool {
	switch e.StatusCode {
	case gohttp.StatusRequestTimeout, gohttp.StatusLocked, gohttp.StatusTooManyRequests:
		return true
	}
	return e.StatusCode >= gohttp.StatusInternalServerError
}
