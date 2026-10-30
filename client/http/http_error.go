package http

import (
	"fmt"
	gohttp "net/http"
)

type Error struct {
	Method, Url  string
	StatusCode   int
	ResponseBody []byte
}

func (e Error) Error() string {
	message := fmt.Sprintf("http error %d, response '%s'", e.StatusCode, string(e.ResponseBody))
	if e.Method == "" {
		return message
	}
	return e.Method + " " + e.Url + ": " + message
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
