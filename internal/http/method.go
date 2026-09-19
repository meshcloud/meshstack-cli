package http

import gohttp "net/http"

// The request methods this repository sends, re-exported so that a caller of DoRequest needs no
// second import for them. Add one here when a caller starts sending it.
const (
	MethodGet    = gohttp.MethodGet
	MethodPost   = gohttp.MethodPost
	MethodPut    = gohttp.MethodPut
	MethodDelete = gohttp.MethodDelete
)
