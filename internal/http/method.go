package http

import gohttp "net/http"

// The request methods this repository sends, re-exported so that naming one costs no second
// import. A caller of DoRequest passes a method and nothing else from net/http, so without
// these it would import net/http under an alias for four string constants.
//
// Only the four in use. A verb nothing sends would be a list entry somebody has to keep
// honest, and net/http is one import away for whoever adds the fifth.
const (
	MethodGet    = gohttp.MethodGet
	MethodPost   = gohttp.MethodPost
	MethodPut    = gohttp.MethodPut
	MethodDelete = gohttp.MethodDelete
)
