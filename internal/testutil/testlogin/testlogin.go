// Package testlogin logs a command under test in to a fake meshStack.
package testlogin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/fakemeshstack"
)

// LoggedInTo points the CLI at endpoint with [fakemeshstack.Token], and clears every other setting
// the environment of the test run could carry into the resolution.
func LoggedInTo(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv(config.DirectorySetting.EnvKey(), t.TempDir())
	t.Setenv(meshstack.EndpointSetting.EnvKey(), endpoint)
	t.Setenv(meshstack.SkipVersionCheckSetting.EnvKey(), "true")
	t.Setenv(meshstack.WorkspaceSetting.EnvKey(), "")
	t.Setenv(profile.NameSetting.EnvKey(), "")
	t.Setenv(auth.ApiKeyClientIdSetting.EnvKey(), "")
	t.Setenv(auth.ApiKeyClientSecretSetting.EnvKey(), "")
	t.Setenv(auth.ApiTokenSetting.EnvKey(), fakemeshstack.Token)
}

// HoldProfiles holds the profiles as a command that changes them does: meshstack profile while it is
// open, and a login until the browser comes back.
func HoldProfiles(t *testing.T) (release func()) {
	t.Helper()
	held, err := profile.LoadProfiles(t.Context(), profile.LoadProfilesOptions{ExclusiveLock: true})
	require.NoError(t, err)
	return func() { require.NoError(t, held.Unlock()) }
}
