package setting_test

import (
	"context"
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type LookupFunc func(ctx context.Context, key string) (string, error)

func (l LookupFunc) Lookup(ctx context.Context, key string) (string, error) {
	return l(ctx, key)
}

func (l LookupFunc) Describe(key string) string {
	return fmt.Sprintf("%T for %s", l, key)
}

var _ setting.Source = LookupFunc(nil)
