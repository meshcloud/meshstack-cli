package io

import (
	"context"
	"io"

	internal "github.com/meshcloud/meshstack-cli/internal/io"
)

// WithStderr carries the writer that a login writes what a person has to read to, such as the
// authorization URL of a browser login. A front end stamps its own stderr, a test its own buffer,
// and a context that carries none leaves os.Stderr.
func WithStderr(ctx context.Context, stderr io.Writer) context.Context {
	return internal.WithStderr(ctx, stderr)
}
