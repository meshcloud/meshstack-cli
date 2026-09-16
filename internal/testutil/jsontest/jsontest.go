// Package jsontest holds the json helpers that only a test needs.
package jsontest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

func MustUnmarshal[T any](t *testing.T, in []byte) (out T) {
	t.Helper()
	require.NoError(t, json.Unmarshal(in, &out))
	return
}
