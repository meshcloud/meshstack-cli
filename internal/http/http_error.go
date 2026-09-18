package http

import (
	"fmt"
	gohttp "net/http"
)

// Error represents an HTTP error response with status code.
// This error is returned when an HTTP request fails with a non-2XX status code.
type Error struct {
	StatusCode   int
	ResponseBody []byte
}

func (e Error) Error() string {
	return fmt.Sprintf("http error %d, response '%s'", e.StatusCode, string(e.ResponseBody))
}

// IsUnauthorized returns true if the error is a 401 Unauthorized response.
func (e Error) IsUnauthorized() bool {
	return e.StatusCode == gohttp.StatusUnauthorized
}

// IsForbidden returns true if the error is a 403 Forbidden response.
func (e Error) IsForbidden() bool {
	return e.StatusCode == gohttp.StatusForbidden
}

// IsNotFound returns true if the error is a 404 Not Found response.
func (e Error) IsNotFound() bool {
	return e.StatusCode == gohttp.StatusNotFound
}

// IsConflict returns true if the error is a 409 Conflict response.
func (e Error) IsConflict() bool {
	return e.StatusCode == gohttp.StatusConflict
}
