package jsontest

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

func MustUnmarshal[T any](t *testing.T, in []byte) (out T) {
	t.Helper()
	require.NoError(t, json.UnmarshalRead(bytes.NewReader(in), &out))
	return
}
