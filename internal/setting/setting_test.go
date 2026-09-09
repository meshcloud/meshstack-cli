package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testTextUnmarshaler string

func (u *testTextUnmarshaler) UnmarshalText(text []byte) error {
	*u = testTextUnmarshaler(text)
	return nil
}

func TestParseTextUnmarshaler(t *testing.T) {
	parsed, err := ParseTextUnmarshaler[testTextUnmarshaler]("test")
	require.NoError(t, err)
	assert.Equal(t, testTextUnmarshaler("test"), parsed)
}
