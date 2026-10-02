package auth

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/testutil/testlogin"
)

func TestLoginFailsWhileMeshstackProfileHoldsTheProfiles(t *testing.T) {
	withApiToken(t)
	release := testlogin.HoldProfiles(t)

	synctest.Test(t, func(t *testing.T) {
		_, err := execute(t, "login", "--apitoken")

		require.ErrorIs(t, err, profile.ErrInUse)
		require.EqualError(t, err, "cannot log in while another meshstack command is changing the profiles, "+
			"such as meshstack profile or another login; quit it and try again")
	})
	dir, err := profile.ResolveProfileOptions{}.ResolveSetting(t.Context(), config.DirectorySetting)
	require.NoError(t, err)
	assert.NoFileExists(t, dir.ProfilesJson(), "a login that waits stores nothing")

	release()
	_, err = execute(t, "login", "--apitoken")
	require.NoError(t, err)
	assert.FileExists(t, dir.ProfilesJson(), "a login stores the profiles once the lock is free")
	testlogin.HoldProfiles(t)()
}
