package setting_test

import (
	"fmt"
	"strconv"
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

func TestParseBool(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", true}, // should never happen, as Setting.Resolve skips empty strings
		// consistent with Go's stringified bool
		{strconv.FormatBool(false), false},
		{strconv.FormatBool(true), true},
		// other false's
		{"FALSE", false},
		{"n", false},
		{"0", false},
		{"N", false},
		// other true's (essentially any string)
		{"TRUE", true},
		{"y", true},
		{"1", true},
		{"Y", true},
		{"YEZZZ", true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("in '%s'", tt.in), func(t *testing.T) {
			got, err := setting.ParseBool(tt.in)
			require.NoError(t, err)
			assert.Equalf(t, tt.want, got, "ParseBool(%v)", tt.in)
		})
	}
}
