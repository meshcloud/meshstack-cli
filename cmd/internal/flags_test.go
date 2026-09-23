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

func TestListWorkspaceTakesTheFlagOverTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, flag, env string
		want            *string
	}{
		{name: "flag", flag: "from-flag", want: new("from-flag")},
		{name: "environment", env: "from-env", want: new("from-env")},
		{name: "both", flag: "from-flag", env: "from-env", want: new("from-flag")},
		{name: "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(meshstack.WorkspaceSetting.EnvKey(), tc.env)
			internal.WorkspaceFlag.Value = tc.flag
			t.Cleanup(func() { internal.WorkspaceFlag.Value = "" })

			workspace, err := internal.ListWorkspace(t.Context())

			require.NoError(t, err)
			assert.Equal(t, tc.want, workspace)
		})
	}
}
