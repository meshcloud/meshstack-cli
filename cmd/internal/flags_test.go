package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

func TestTheEnvironmentDecidesWhileTheBoolFlagIsUnset(t *testing.T) {
	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")

	skip, err := internal.SettingSources().ResolveSetting(t.Context(), meshstack.SkipVersionCheckSetting)

	require.NoError(t, err)
	assert.True(t, skip)
}
