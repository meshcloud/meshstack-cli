package setting_test

import (
	"fmt"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type LookupFunc func(key string) (string, error)

func (l LookupFunc) Lookup(key string) (string, error) {
	return l(key)
}

func (l LookupFunc) Describe(key string) string {
	return fmt.Sprintf("%T for %s", l, key)
}

var _ setting.Source = LookupFunc(nil)
