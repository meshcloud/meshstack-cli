package setting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func TestParseBoolReadsOnlyASpellingOfNoAsFalse(t *testing.T) {
	falseSpellings := []string{"false", "FALSE", "no", "n", "N", "0"}
	for _, spelling := range falseSpellings {
		t.Run(spelling, func(t *testing.T) {
			parsed, err := setting.ParseBool(spelling)
			require.NoError(t, err)
			assert.False(t, parsed)
		})
	}

	trueSpellings := []string{"true", "TRUE", "yes", "y", "1", "whatever-else"}
	for _, spelling := range trueSpellings {
		t.Run(spelling, func(t *testing.T) {
			parsed, err := setting.ParseBool(spelling)
			require.NoError(t, err)
			assert.True(t, parsed)
		})
	}
}
