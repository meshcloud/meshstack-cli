package setting

import (
	"fmt"
	"os"
)

type EnvKey string

func (k EnvKey) Lookup(key string) (string, error) {
	if k != EnvKey(key) {
		// not self-sourcing a Value is an implementation bug for now
		// it might become relevant when we migrate env keys and define aliases/fallbacks
		panic(fmt.Sprintf("env key %s does not match %s", k, key))
	}
	return os.Getenv(string(k)), nil
}

func (k EnvKey) Describe(key string) string {
	return "environment variable " + key
}

var _ Source = EnvKey("")
