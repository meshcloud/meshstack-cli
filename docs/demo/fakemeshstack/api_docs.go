package main

import (
	"bytes"
	_ "embed"
	"net/http"
	"time"
)

// apiDocs is meshStack's OpenAPI document cut down to a few building block operations with a few
// fields each, so that a description of them fits the gif.
//
//go:embed api-docs.json
var apiDocs []byte

const (
	docsHost    = "docs.meshstack.example.com"
	apiDocsPath = "/api/meshstack-openapi-docs.json"
)

// serveApiDocs answers the CLI's conditional GET by the time the server started.
func serveApiDocs(modified time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeContent(w, r, "", modified, bytes.NewReader(apiDocs))
	}
}
