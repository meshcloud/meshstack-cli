package io

import (
	"context"
	"io"

	internal "github.com/meshcloud/meshstack-cli/internal/io"
)

// WithStderr sets the writer a login sends what a person has to read to, such as the authorization
// URL of a browser login. A context carrying none leaves os.Stderr.
func WithStderr(ctx context.Context, stderr io.Writer) context.Context {
	return internal.WithStderr(ctx, stderr)
}
