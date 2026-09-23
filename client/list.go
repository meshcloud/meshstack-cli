package client

import (
	"context"

	"github.com/meshcloud/meshstack-cli/client/internal"
)

// ListOptions shape how every listing on a context fetches its pages.
type ListOptions = internal.ListOptions

// Page is what a page of a listing says about the listing as a whole.
type Page = internal.Page

func WithListOptions(ctx context.Context, options ListOptions) context.Context {
	return internal.WithListOptions(ctx, options)
}

// ListOptionsFrom returns the options [WithListOptions] put on ctx, and the zero value without.
func ListOptionsFrom(ctx context.Context) ListOptions {
	return internal.ListOptionsFrom(ctx)
}
