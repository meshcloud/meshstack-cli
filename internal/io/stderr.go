package io

import (
	"context"
	"io"
	"os"
)

type stderrKey struct{}

func WithStderr(ctx context.Context, stderr io.Writer) context.Context {
	return context.WithValue(ctx, stderrKey{}, stderr)
}

func Stderr(ctx context.Context) io.Writer {
	if stderr, ok := ctx.Value(stderrKey{}).(io.Writer); ok {
		return stderr
	}
	return os.Stderr
}
