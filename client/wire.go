package client

import "encoding/json/v2"

// wireCompatibility repeats the options internal/json marshals every request with: a v1-style
// MarshalJSON receives none of its caller's, so without it a nested value would go out in a
// different shape than the request around it.
var wireCompatibility = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)
