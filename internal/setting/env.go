package setting

import (
	"context"
	"fmt"
	"os"
)

type EnvKey string

func (k EnvKey) Lookup(_ context.Context, key string) (string, error) {
	if k != EnvKey(key) {
		// A panic, not an error: an EnvKey asked for another setting's key can only be a wiring
		// mistake, and returning empty would hide it as a missing value.
		panic(fmt.Sprintf("env key %s does not match %s", k, key))
	}
	return os.Getenv(string(k)), nil
}

func (k EnvKey) Describe(key string) string {
	return "environment variable " + key
}

var _ Source = EnvKey("")
