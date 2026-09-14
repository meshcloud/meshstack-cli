package setting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type testTextUnmarshaler string

func (u *testTextUnmarshaler) UnmarshalText(text []byte) error {
	*u = testTextUnmarshaler(text)
	return nil
}

func TestParseTextUnmarshaler(t *testing.T) {
	parsed, err := setting.ParseTextUnmarshaler[testTextUnmarshaler]("test")
	require.NoError(t, err)
	assert.Equal(t, testTextUnmarshaler("test"), parsed)
}
